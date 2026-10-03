package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/target"
)

// Only name and spec are accepted. Clients cannot supply status or metadata that
// would bypass admission, readiness, ownership, or deletion controls.
type CreateClassRequest struct {
	Name string                        `json:"name"`
	Spec platform.EnvironmentClassSpec `json:"spec"`
}

type ClassDetail struct {
	ClassView
	Spec       platform.EnvironmentClassSpec `json:"spec"`
	Conditions []metav1.Condition            `json:"conditions"`
	SpecDigest string                        `json:"specDigest,omitempty"`
	Redacted   bool                          `json:"redacted"`
	YAML       string                        `json:"yaml"`
}

type ClassValidation struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
	YAML     string   `json:"yaml,omitempty"`
}

func classObject(request CreateClassRequest) *platform.EnvironmentClass {
	return &platform.EnvironmentClass{
		TypeMeta:   metav1.TypeMeta{APIVersion: platform.GroupVersion.String(), Kind: "EnvironmentClass"},
		ObjectMeta: metav1.ObjectMeta{Name: request.Name, Labels: map[string]string{"platform.example.io/managed-by": "platform-api"}},
		Spec:       request.Spec,
	}
}

func (s *Server) classUsage(ctx context.Context) (map[string]int32, error) {
	var environments platform.PlatformEnvironmentList
	if err := s.client.List(ctx, &environments); err != nil {
		return nil, err
	}
	usage := make(map[string]int32)
	for _, environment := range environments.Items {
		if environment.Spec.ClassRef != nil {
			usage[environment.Spec.ClassRef.Name]++
		}
	}
	return usage, nil
}

func classDetail(class *platform.EnvironmentClass, usage int32) ClassDetail {
	safe := class.DeepCopy()
	redacted := redactClassSpec(&safe.Spec)
	view := classView(class)
	view.UsageCount = usage
	manifest := classObject(CreateClassRequest{Name: safe.Name, Spec: safe.Spec})
	content, _ := yaml.Marshal(manifest)
	return ClassDetail{ClassView: view, Spec: safe.Spec, Conditions: safe.Status.Conditions, SpecDigest: safe.Status.SpecDigest, Redacted: redacted, YAML: string(content)}
}

func (s *Server) getClass(w http.ResponseWriter, r *http.Request) {
	var class platform.EnvironmentClass
	if err := s.client.Get(r.Context(), client.ObjectKey{Name: r.PathValue("name")}, &class); err != nil {
		s.writeObjectError(w, err, "EnvironmentClass")
		return
	}
	usage, err := s.classUsage(r.Context())
	if err != nil {
		s.writeObjectError(w, err, "PlatformEnvironment")
		return
	}
	writeJSON(w, http.StatusOK, classDetail(&class, usage[class.Name]))
}

func (s *Server) validateClass(w http.ResponseWriter, r *http.Request) {
	var request CreateClassRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result := validateClassRequest(request)
	if result.Valid {
		// Admission validates the repository CRD and detects duplicate names;
		// dry-run never persists the class or starts an environment.
		if err := s.client.Create(r.Context(), classObject(request), client.DryRunAll); err != nil {
			if apierrors.IsInvalid(err) || apierrors.IsAlreadyExists(err) || apierrors.IsBadRequest(err) {
				result.Valid = false
				result.Errors = append(result.Errors, err.Error())
			} else {
				s.writeClassMutationError(w, err)
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) createClass(w http.ResponseWriter, r *http.Request) {
	var request CreateClassRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result := validateClassRequest(request)
	if !result.Valid {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"code": "ClassRejected", "message": strings.Join(result.Errors, "; "), "errors": result.Errors})
		return
	}
	class := classObject(request)
	if err := s.client.Create(r.Context(), class); err != nil {
		s.writeClassMutationError(w, err)
		return
	}
	w.Header().Set("Location", "/api/classes/"+class.Name)
	writeJSON(w, http.StatusCreated, classDetail(class, 0))
}

func (s *Server) deleteClass(w http.ResponseWriter, r *http.Request) {
	var request struct {
		UID         string `json:"uid"`
		ConfirmName string `json:"confirmName"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	name := r.PathValue("name")
	if request.UID == "" || request.ConfirmName != name {
		writeError(w, http.StatusBadRequest, "ClassConfirmationRequired", "UID and exact class name confirmation are required")
		return
	}
	var class platform.EnvironmentClass
	if err := s.client.Get(r.Context(), client.ObjectKey{Name: name}, &class); err != nil {
		s.writeObjectError(w, err, "EnvironmentClass")
		return
	}
	if string(class.UID) != request.UID {
		writeError(w, http.StatusConflict, "StaleClass", "EnvironmentClass identity changed; refresh before deleting")
		return
	}
	usage, err := s.classUsage(r.Context())
	if err != nil {
		s.writeObjectError(w, err, "PlatformEnvironment")
		return
	}
	if usage[name] > 0 {
		writeError(w, http.StatusConflict, "ClassInUse", fmt.Sprintf("EnvironmentClass is referenced by %d environments", usage[name]))
		return
	}
	uid := types.UID(request.UID)
	if err := s.client.Delete(r.Context(), &class, client.Preconditions{UID: &uid, ResourceVersion: &class.ResourceVersion}); err != nil {
		s.writeClassMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"name": name, "state": "Deleting"})
}

func (s *Server) writeClassMutationError(w http.ResponseWriter, err error) {
	switch {
	case apierrors.IsAlreadyExists(err):
		writeError(w, http.StatusConflict, "ClassAlreadyExists", "A class with this name already exists; choose a new name or version")
	case apierrors.IsConflict(err):
		writeError(w, http.StatusConflict, "StaleClass", err.Error())
	case apierrors.IsInvalid(err), apierrors.IsBadRequest(err):
		writeError(w, http.StatusUnprocessableEntity, "ClassRejected", err.Error())
	case apierrors.IsForbidden(err):
		writeError(w, http.StatusForbidden, "ClassForbidden", err.Error())
	case apierrors.IsNotFound(err):
		writeError(w, http.StatusNotFound, "NotFound", "EnvironmentClass was not found")
	default:
		writeError(w, http.StatusServiceUnavailable, "KubernetesWriteFailed", err.Error())
	}
}

var pinnedRevision = regexp.MustCompile(`^[0-9a-fA-F]{40,64}$`)

func validateClassRequest(request CreateClassRequest) ClassValidation {
	result := ClassValidation{Errors: []string{}, Warnings: []string{"Backend configuration must exist in each environment namespace. Ready confirms class admission and controller reconciliation; source access, runner images and infrastructure execution are checked later."}}
	require := func(field, value string) {
		if strings.TrimSpace(value) == "" {
			result.Errors = append(result.Errors, field+" is required")
		}
	}
	if problems := validation.IsDNS1123Subdomain(request.Name); len(problems) > 0 {
		result.Errors = append(result.Errors, "name: "+strings.Join(problems, ", "))
	}
	spec := classObject(request).DeepCopy().Spec
	require("version", spec.Version)
	require("source.url", spec.Source.URL)
	if !pinnedRevision.MatchString(spec.Source.Revision) {
		result.Errors = append(result.Errors, "source.revision must be a pinned 40-64 character hexadecimal commit")
	}
	if spec.Source.Path == "" || strings.Contains(spec.Source.Path, "\\") || path.IsAbs(spec.Source.Path) || path.Clean(spec.Source.Path) == ".." || strings.HasPrefix(path.Clean(spec.Source.Path), "../") {
		result.Errors = append(result.Errors, "source.path must be a relative path inside the repository")
	}
	if spec.Backend.Type != "s3" {
		result.Errors = append(result.Errors, "backend.type must be s3")
	}
	for field, value := range map[string]string{"backend.configRef.name": spec.Backend.ConfigRef.Name, "backend.authRef.serviceAccountName": spec.Backend.AuthRef.ServiceAccountName, "runnerProfile.serviceAccountName": spec.RunnerProfile.ServiceAccountName} {
		if problems := validation.IsDNS1123Subdomain(value); len(problems) > 0 {
			result.Errors = append(result.Errors, field+": "+strings.Join(problems, ", "))
		}
	}
	if spec.Backend.AuthRef.ServiceAccountName != spec.RunnerProfile.ServiceAccountName {
		result.Errors = append(result.Errors, "backend and runner service accounts must match")
	}
	if spec.Backend.LockTimeout.Duration <= 0 {
		result.Errors = append(result.Errors, "backend.lockTimeout must be positive")
	}
	if spec.Executor.ExecutionTimeout.Duration <= 0 {
		result.Errors = append(result.Errors, "executor.executionTimeout must be positive")
	}
	require("executor.image", spec.Executor.Image)
	require("executor.terraformVersion", spec.Executor.TerraformVersion)
	require("executor.workDir", spec.Executor.WorkDir)
	require("runnerProfile.image", spec.RunnerProfile.Image)
	require("runnerProfile.imageDigest", spec.RunnerProfile.ImageDigest)
	require("runnerProfile.terraformVersion", spec.RunnerProfile.TerraformVersion)
	if spec.Executor.TerraformVersion != spec.RunnerProfile.TerraformVersion || spec.Executor.Image != spec.RunnerProfile.Image {
		result.Errors = append(result.Errors, "executor and runner image / Terraform version must match")
	}
	if spec.Executor.Parallelism != nil && *spec.Executor.Parallelism < 1 {
		result.Errors = append(result.Errors, "executor.parallelism must be positive")
	}
	t := spec.Target
	if _, err := target.MaterializeTarget(target.TargetExpectation{Provider: t.Provider, AccountID: t.Account, Region: t.Region, ClusterName: t.ClusterName, ClusterID: t.ClusterID, ClusterARN: t.ClusterARN, IncarnationID: t.IncarnationID, ConnectionProfile: t.ConnectionProfileRef, ExecutionRoleARN: t.ExecutionRoleARN, RuntimeRoleARN: t.RuntimeRoleARN}); err != nil {
		result.Errors = append(result.Errors, err.Error())
	}
	require("target.region", t.Region)
	seen := map[string]bool{}
	for _, region := range spec.AllowedRegions {
		if strings.TrimSpace(region) == "" || seen[region] {
			result.Errors = append(result.Errors, "allowedRegions must contain unique non-empty regions")
		}
		seen[region] = true
	}
	if len(spec.AllowedRegions) > 0 && !seen[t.Region] {
		result.Errors = append(result.Errors, "target.region must be included in allowedRegions")
	}
	b := spec.CapacityBounds
	if b.MinNodeCount < 0 || b.MaxNodeCount < 0 || b.MaxEnvironments < 0 || b.MaxConcurrentPlans < 0 || b.MaxConcurrentApplies < 0 {
		result.Errors = append(result.Errors, "capacity limits cannot be negative")
	}
	if b.MaxNodeCount > 0 && b.MinNodeCount > b.MaxNodeCount {
		result.Errors = append(result.Errors, "minimum node count cannot exceed maximum")
	}
	if spec.ApprovalPolicy != platform.ApprovalPolicyManual && spec.ApprovalPolicy != platform.ApprovalPolicyAutomatic {
		result.Errors = append(result.Errors, "approvalPolicy must be Manual or Automatic")
	}
	for i, object := range spec.RuntimeProfile.RuntimeObjects {
		if object.Version == "" || object.Resource == "" || len(object.Object.Raw) == 0 {
			result.Errors = append(result.Errors, fmt.Sprintf("runtimeObjects[%d] requires version, resource and object", i))
		}
		var manifest map[string]any
		if err := json.Unmarshal(object.Object.Raw, &manifest); err != nil || manifest == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("runtimeObjects[%d].object must be a JSON object", i))
		}
	}
	if redactClassSpec(&spec) {
		result.Errors = append(result.Errors, "Inline credentials and Secret resources are not accepted; use credential references")
	}
	result.Valid = len(result.Errors) == 0
	sort.Strings(result.Errors)
	if result.Valid {
		content, _ := yaml.Marshal(classObject(request))
		result.YAML = string(content)
	}
	return result
}

// Existing classes may contain legacy inline credentials. Keep those values out
// of the management read model and disable cloning a redacted specification.
func redactClassSpec(spec *platform.EnvironmentClassSpec) bool {
	redacted := false
	if parsed, err := url.Parse(spec.Source.URL); err == nil && parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(parsed.User.Username(), "[redacted]")
			spec.Source.URL = parsed.String()
			redacted = true
		}
	}
	if parsed, err := url.Parse(spec.Source.URL); err == nil {
		query := parsed.Query()
		for key := range query {
			if sensitiveClassKey(key) {
				query.Set(key, "[redacted]")
				redacted = true
			}
		}
		parsed.RawQuery = query.Encode()
		if redacted {
			spec.Source.URL = parsed.String()
		}
	}
	for i := range spec.RuntimeProfile.RuntimeObjects {
		object := &spec.RuntimeProfile.RuntimeObjects[i]
		var value map[string]any
		if json.Unmarshal(object.Object.Raw, &value) != nil {
			continue
		}
		if strings.EqualFold(fmt.Sprint(value["kind"]), "Secret") || object.Resource == "secrets" {
			value = map[string]any{"kind": "Secret", "redacted": true}
			redacted = true
		} else if redactClassValue(value) {
			redacted = true
		}
		object.Object.Raw, _ = json.Marshal(value)
	}
	return redacted
}

func redactClassValue(value any) bool {
	redacted := false
	switch item := value.(type) {
	case map[string]any:
		if name, ok := item["name"].(string); ok && sensitiveClassKey(name) {
			if _, inline := item["value"]; inline {
				item["value"] = "[redacted]"
				redacted = true
			}
		}
		for key, child := range item {
			text, isString := child.(string)
			if sensitiveClassKey(key) && isString && text != "" {
				item[key] = "[redacted]"
				redacted = true
			} else {
				if redactClassValue(child) {
					redacted = true
				}
			}
		}
	case []any:
		for _, child := range item {
			if redactClassValue(child) {
				redacted = true
			}
		}
	}
	return redacted
}

func sensitiveClassKey(key string) bool {
	key = strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
	if strings.HasSuffix(key, "ref") || strings.HasSuffix(key, "refs") || key == "serviceaccounttoken" || key == "automountserviceaccounttoken" {
		return false
	}
	for _, fragment := range []string{"password", "passwd", "token", "privatekey", "secretaccesskey", "accesskeyid", "authorization", "clientsecret"} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}
