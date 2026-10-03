package console

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
)

// Templates are reusable configuration snapshots, independent of admitted
// EnvironmentClasses. Saving one never creates an environment or Terraform run.
const classTemplateNamespace = "default"
const classTemplatePrefix = "pcp-class-template-"
const classTemplateLabel = "platform.example.io/configuration-kind"

type ClassTemplateRequest struct {
	Name        string                        `json:"name"`
	Title       string                        `json:"title"`
	Description string                        `json:"description"`
	Spec        platform.EnvironmentClassSpec `json:"spec"`
}

type ClassTemplate struct {
	ClassTemplateRequest
	UID       string `json:"uid"`
	CreatedAt string `json:"createdAt"`
	Redacted  bool   `json:"redacted"`
}

func isClassTemplate(config *corev1.ConfigMap) bool {
	return config.Namespace == classTemplateNamespace && strings.HasPrefix(config.Name, classTemplatePrefix) &&
		config.Labels[classTemplateLabel] == "class-template" && config.Labels["platform.example.io/managed-by"] == "platform-api"
}

func templateView(config *corev1.ConfigMap) (ClassTemplate, error) {
	var value ClassTemplateRequest
	if err := json.Unmarshal([]byte(config.Data["template.json"]), &value); err != nil {
		return ClassTemplate{}, err
	}
	// Identity comes from the Kubernetes object, never from its editable data.
	value.Name = strings.TrimPrefix(config.Name, classTemplatePrefix)
	redacted := redactClassSpec(&value.Spec)
	return ClassTemplate{ClassTemplateRequest: value, UID: string(config.UID), CreatedAt: config.CreationTimestamp.UTC().Format("2006-01-02T15:04:05Z"), Redacted: redacted}, nil
}

func (s *Server) listClassTemplates(w http.ResponseWriter, r *http.Request) {
	var configs corev1.ConfigMapList
	if err := s.client.List(r.Context(), &configs, client.InNamespace(classTemplateNamespace), client.MatchingLabels{classTemplateLabel: "class-template", "platform.example.io/managed-by": "platform-api"}); err != nil {
		s.writeObjectError(w, err, "ClassTemplate")
		return
	}
	values := make([]ClassTemplate, 0, len(configs.Items))
	for _, config := range configs.Items {
		if !isClassTemplate(&config) {
			continue
		}
		value, err := templateView(&config)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "TemplateUnreadable", "A saved template contains invalid configuration: "+config.Name)
			return
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	writeJSON(w, http.StatusOK, values)
}

func (s *Server) createClassTemplate(w http.ResponseWriter, r *http.Request) {
	var request ClassTemplateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.Title = strings.TrimSpace(request.Title)
	request.Description = strings.TrimSpace(request.Description)
	problems := validation.IsDNS1123Label(request.Name)
	if request.Title == "" || len(request.Title) > 120 {
		problems = append(problems, "title is required and must not exceed 120 bytes")
	}
	if len(request.Description) > 1000 {
		problems = append(problems, "description must not exceed 1000 bytes")
	}
	result := validateClassRequest(CreateClassRequest{Name: request.Name, Spec: request.Spec})
	problems = append(problems, result.Errors...)
	if len(problems) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "TemplateRejected", strings.Join(problems, "; "))
		return
	}
	content, err := json.Marshal(request)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "TemplateRejected", "Template configuration could not be encoded")
		return
	}
	config := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: classTemplateNamespace, Name: classTemplatePrefix + request.Name, Labels: map[string]string{classTemplateLabel: "class-template", "platform.example.io/managed-by": "platform-api"}},
		Data:       map[string]string{"template.json": string(content)},
	}
	if err := s.client.Create(r.Context(), config); err != nil {
		if apierrors.IsAlreadyExists(err) {
			writeError(w, http.StatusConflict, "TemplateAlreadyExists", "A template with this name already exists; choose a new name")
		} else {
			s.writeObjectError(w, err, "ClassTemplate")
		}
		return
	}
	value, err := templateView(config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TemplateUnreadable", "Template was saved but could not be read")
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (s *Server) deleteClassTemplate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		UID         string `json:"uid"`
		ConfirmName string `json:"confirmName"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	name := r.PathValue("name")
	if len(validation.IsDNS1123Label(name)) > 0 || request.UID == "" || request.ConfirmName != name {
		writeError(w, http.StatusBadRequest, "TemplateConfirmationRequired", "UID and exact template name confirmation are required")
		return
	}
	var config corev1.ConfigMap
	if err := s.client.Get(r.Context(), client.ObjectKey{Namespace: classTemplateNamespace, Name: classTemplatePrefix + name}, &config); err != nil {
		s.writeObjectError(w, err, "ClassTemplate")
		return
	}
	if !isClassTemplate(&config) {
		writeError(w, http.StatusNotFound, "NotFound", "ClassTemplate was not found")
		return
	}
	if string(config.UID) != request.UID {
		writeError(w, http.StatusConflict, "StaleTemplate", "Template identity changed; refresh before deleting")
		return
	}
	uid := types.UID(request.UID)
	if err := s.client.Delete(r.Context(), &config, client.Preconditions{UID: &uid, ResourceVersion: &config.ResourceVersion}); err != nil {
		s.writeObjectError(w, err, "ClassTemplate")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name, "state": "Deleted"})
}
