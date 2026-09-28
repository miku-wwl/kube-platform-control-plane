package planview

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Document is a deliberately small, secret-filtered projection of Terraform's
// plan JSON. It is explanatory UI evidence and is never consumed by Apply.
type Document struct {
	Version    int     `json:"version"`
	RunUID     string  `json:"terraformRunUID"`
	PlanDigest string  `json:"planDigest"`
	Nodes      []Node  `json:"nodes"`
	Edges      []Edge  `json:"edges"`
	Summary    Summary `json:"summary"`
}

type Node struct {
	Address  string         `json:"address"`
	Type     string         `json:"type"`
	Provider string         `json:"provider,omitempty"`
	Action   string         `json:"action"`
	Before   map[string]any `json:"before,omitempty"`
	After    map[string]any `json:"after,omitempty"`
}

type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Summary struct {
	Create  int `json:"create"`
	Update  int `json:"update"`
	Delete  int `json:"delete"`
	Replace int `json:"replace"`
	NoOp    int `json:"noOp"`
}

type rawPlan struct {
	FormatVersion   string `json:"format_version"`
	ResourceChanges []struct {
		Address      string `json:"address"`
		Type         string `json:"type"`
		ProviderName string `json:"provider_name"`
		Change       struct {
			Actions         []string       `json:"actions"`
			Before          map[string]any `json:"before"`
			After           map[string]any `json:"after"`
			BeforeSensitive any            `json:"before_sensitive"`
			AfterSensitive  any            `json:"after_sensitive"`
		} `json:"change"`
	} `json:"resource_changes"`
	Configuration struct {
		RootModule module `json:"root_module"`
	} `json:"configuration"`
}

type module struct {
	Address   string `json:"address"`
	Resources []struct {
		Address     string `json:"address"`
		Expressions map[string]struct {
			References []string `json:"references"`
		} `json:"expressions"`
	} `json:"resources"`
	ChildModules []module `json:"child_modules"`
}

var metadataAttributes = map[string]struct{}{
	"name": {}, "id": {}, "bucket": {}, "region": {}, "availability_zone": {},
	"instance_type": {}, "cidr_block": {}, "engine": {}, "engine_version": {},
	"replicas": {}, "port": {}, "tags": {}, "arn": {},
}

// Parse converts the exact terraform show -json output associated with a
// saved plan into a stable, allowlisted visualization document.
func Parse(data []byte, runUID, planDigest string) (Document, error) {
	var source rawPlan
	if err := json.Unmarshal(data, &source); err != nil {
		return Document{}, fmt.Errorf("decode Terraform plan JSON: %w", err)
	}
	if source.FormatVersion != "" {
		major := strings.SplitN(source.FormatVersion, ".", 2)[0]
		if major != "1" {
			return Document{}, fmt.Errorf("unsupported Terraform plan JSON major version %q", major)
		}
	}
	doc := Document{Version: 1, RunUID: runUID, PlanDigest: planDigest, Nodes: make([]Node, 0, len(source.ResourceChanges)), Edges: []Edge{}}
	for _, resource := range source.ResourceChanges {
		if resource.Address == "" || resource.Type == "" {
			continue
		}
		action := classify(resource.Change.Actions)
		node := Node{
			Address: resource.Address, Type: resource.Type, Provider: resource.ProviderName, Action: action,
			Before: safeMetadata(resource.Change.Before, resource.Change.BeforeSensitive),
			After:  safeMetadata(resource.Change.After, resource.Change.AfterSensitive),
		}
		doc.Nodes = append(doc.Nodes, node)
		switch action {
		case "create":
			doc.Summary.Create++
		case "update":
			doc.Summary.Update++
		case "delete":
			doc.Summary.Delete++
		case "replace":
			doc.Summary.Replace++
		default:
			doc.Summary.NoOp++
		}
	}
	sort.Slice(doc.Nodes, func(i, j int) bool { return doc.Nodes[i].Address < doc.Nodes[j].Address })
	known := make(map[string]struct{}, len(doc.Nodes))
	for _, node := range doc.Nodes {
		known[node.Address] = struct{}{}
	}
	edges := map[string]Edge{}
	collectEdges(source.Configuration.RootModule, known, edges)
	for _, edge := range edges {
		doc.Edges = append(doc.Edges, edge)
	}
	sort.Slice(doc.Edges, func(i, j int) bool {
		if doc.Edges[i].From == doc.Edges[j].From {
			return doc.Edges[i].To < doc.Edges[j].To
		}
		return doc.Edges[i].From < doc.Edges[j].From
	})
	return doc, nil
}

func classify(actions []string) string {
	switch {
	case len(actions) == 2 && ((actions[0] == "delete" && actions[1] == "create") || (actions[0] == "create" && actions[1] == "delete")):
		return "replace"
	case len(actions) == 1 && actions[0] == "create":
		return "create"
	case len(actions) == 1 && actions[0] == "update":
		return "update"
	case len(actions) == 1 && actions[0] == "delete":
		return "delete"
	default:
		return "no-op"
	}
}

func safeMetadata(values map[string]any, sensitive any) map[string]any {
	result := map[string]any{}
	for key, value := range values {
		if _, ok := metadataAttributes[key]; !ok || isSensitiveKey(key) || markedSensitive(sensitive) || markedSensitive(nestedMask(sensitive, key)) {
			continue
		}
		if key == "tags" {
			tags, ok := value.(map[string]any)
			if !ok {
				continue
			}
			safeTags := map[string]any{}
			for tagKey, tagValue := range tags {
				if isSensitiveKey(tagKey) || markedSensitive(nestedMask(nestedMask(sensitive, key), tagKey)) {
					continue
				}
				if text, ok := tagValue.(string); ok && len(text) <= 128 {
					safeTags[tagKey] = text
				}
			}
			if len(safeTags) > 0 {
				result[key] = safeTags
			}
			continue
		}
		switch v := value.(type) {
		case string:
			if len(v) <= 160 {
				result[key] = v
			}
		case float64, bool, nil:
			result[key] = v
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func markedSensitive(value any) bool { flag, ok := value.(bool); return ok && flag }
func nestedMask(value any, key string) any {
	if fields, ok := value.(map[string]any); ok {
		return fields[key]
	}
	return nil
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", ""))
	for _, part := range []string{"password", "secret", "token", "private", "credential", "accesskey", "secretkey", "certificate", "authorization", "kubeconfig", "userdata"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

func collectEdges(current module, known map[string]struct{}, edges map[string]Edge) {
	for _, resource := range current.Resources {
		if _, exists := known[resource.Address]; !exists {
			continue
		}
		for _, expression := range resource.Expressions {
			for _, reference := range expression.References {
				from := matchingAddress(reference, known)
				if from == "" || from == resource.Address {
					continue
				}
				key := from + "\x00" + resource.Address
				edges[key] = Edge{From: from, To: resource.Address}
			}
		}
	}
	for _, child := range current.ChildModules {
		collectEdges(child, known, edges)
	}
}

func matchingAddress(reference string, known map[string]struct{}) string {
	best := ""
	for address := range known {
		if (reference == address || strings.HasPrefix(reference, address+".") || strings.HasPrefix(reference, address+"[")) && len(address) > len(best) {
			best = address
		}
	}
	return best
}
