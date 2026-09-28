package console

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"k8s.io/apimachinery/pkg/util/validation"
)

type DraftRequest struct {
	Description string `json:"description"`
	Name        string `json:"name,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	ClassRef    string `json:"classRef"`
	Region      string `json:"region,omitempty"`
	NodeCount   int32  `json:"nodeCount,omitempty"`
}

type DraftIntent struct {
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	ClassRef       string `json:"classRef"`
	Region         string `json:"region,omitempty"`
	NodeCount      int32  `json:"nodeCount"`
	ValkeyEnabled  bool   `json:"valkeyEnabled"`
	ValkeyShards   int32  `json:"valkeyShards,omitempty"`
	ValkeyReplicas int32  `json:"valkeyReplicas,omitempty"`
}

type DraftResponse struct {
	Provider string      `json:"provider"`
	Intent   DraftIntent `json:"intent"`
	Valid    bool        `json:"valid"`
	Errors   []string    `json:"errors,omitempty"`
	Warnings []string    `json:"warnings,omitempty"`
	YAML     string      `json:"yaml,omitempty"`
}

type DraftGenerator interface {
	Generate(context.Context, DraftRequest) (DraftIntent, error)
}

type DeterministicDraftGenerator struct{}

func (DeterministicDraftGenerator) Generate(_ context.Context, request DraftRequest) (DraftIntent, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = slugName(request.Description, request.ClassRef)
	}
	namespace := request.Namespace
	if namespace == "" {
		namespace = "default"
	}
	nodeCount := request.NodeCount
	if nodeCount == 0 {
		nodeCount = inferNodeCount(request.Description)
	}
	lower := strings.ToLower(request.Description)
	valkey := strings.Contains(lower, "valkey") || strings.Contains(lower, "redis") || strings.Contains(lower, "cache") || strings.Contains(request.Description, "缓存")
	intent := DraftIntent{Name: name, Namespace: namespace, ClassRef: request.ClassRef, Region: request.Region, NodeCount: nodeCount, ValkeyEnabled: valkey}
	if valkey {
		intent.ValkeyShards = 1
		intent.ValkeyReplicas = 1
	}
	return intent, nil
}

type BedrockDraftGenerator struct {
	client  *bedrockruntime.Client
	modelID string
}

func NewDraftGeneratorFromEnvironment() (string, DraftGenerator, error) {
	provider := strings.ToLower(strings.TrimSpace(env("AI_PROVIDER", "deterministic")))
	switch provider {
	case "deterministic":
		return provider, DeterministicDraftGenerator{}, nil
	case "bedrock":
		endpoint := strings.TrimSpace(env("AWS_ENDPOINT_URL", ""))
		model := strings.TrimSpace(env("BEDROCK_MODEL_ID", "amazon.titan-text-lite-v1"))
		region := strings.TrimSpace(env("AWS_REGION", "us-east-1"))
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return "", nil, fmt.Errorf("invalid AWS_ENDPOINT_URL: %w", err)
		}
		if parsed.Scheme != "http" || parsed.User != nil || !localHost(parsed.Hostname()) {
			return "", nil, errors.New("AI_PROVIDER=bedrock requires AWS_ENDPOINT_URL to be an http loopback address; remote AWS endpoints are refused")
		}
		cfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(region), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")))
		if err != nil {
			return "", nil, fmt.Errorf("configure LocalStack Bedrock SDK: %w", err)
		}
		return provider, &BedrockDraftGenerator{client: bedrockruntime.NewFromConfig(cfg, func(options *bedrockruntime.Options) { options.BaseEndpoint = aws.String(endpoint) }), modelID: model}, nil
	default:
		return "", nil, fmt.Errorf("unsupported AI_PROVIDER %q; use deterministic or bedrock", provider)
	}
}

func (g *BedrockDraftGenerator) Generate(ctx context.Context, request DraftRequest) (DraftIntent, error) {
	if g == nil || g.client == nil {
		return DraftIntent{}, errors.New("Bedrock provider is not configured")
	}
	prompt := fmt.Sprintf(`You are a constrained platform intent drafter. Return exactly one JSON object and no markdown. Never propose commands, manifests, approvals, or execution. Output schema: {"name":"dns-label","region":"","nodeCount":1,"valkeyEnabled":false,"valkeyShards":0,"valkeyReplicas":0}. Do not output namespace or classRef: namespace is %q and the human-selected EnvironmentClass is %q, and the server binds those separately. Respect any explicit name, region, or nodeCount supplied. Use nodeCount 1 for small/development environments. If Valkey is requested, set enabled true, shards 1 and replicas 1. User request: %q. Explicit values: name=%q region=%q nodeCount=%d.`, request.Namespace, request.ClassRef, request.Description, request.Name, request.Region, request.NodeCount)
	response, err := g.client.Converse(ctx, &bedrockruntime.ConverseInput{ModelId: aws.String(g.modelID), Messages: []types.Message{{Role: types.ConversationRoleUser, Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: prompt}}}}})
	if err != nil {
		return DraftIntent{}, fmt.Errorf("LocalStack Bedrock Converse failed: %w", err)
	}
	message, ok := response.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return DraftIntent{}, errors.New("Bedrock returned a non-message response")
	}
	var text string
	for _, block := range message.Value.Content {
		if value, ok := block.(*types.ContentBlockMemberText); ok {
			text += value.Value
		}
	}
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return DraftIntent{}, errors.New("Bedrock response did not contain a JSON object")
	}
	var output struct {
		Name           string `json:"name"`
		Region         string `json:"region"`
		NodeCount      int32  `json:"nodeCount"`
		ValkeyEnabled  bool   `json:"valkeyEnabled"`
		ValkeyShards   int32  `json:"valkeyShards"`
		ValkeyReplicas int32  `json:"valkeyReplicas"`
	}
	decoder := json.NewDecoder(bytes.NewBufferString(text[start : end+1]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return DraftIntent{}, fmt.Errorf("decode Bedrock structured draft: %w", err)
	}
	if request.Name != "" && output.Name != request.Name {
		return DraftIntent{}, errors.New("Bedrock changed the explicitly requested environment name")
	}
	if request.Region != "" && output.Region != request.Region {
		return DraftIntent{}, errors.New("Bedrock changed the explicitly requested region")
	}
	if request.NodeCount > 0 && output.NodeCount != request.NodeCount {
		return DraftIntent{}, errors.New("Bedrock changed the explicitly requested node count")
	}
	if strings.TrimSpace(output.Name) == "" || len(validation.IsDNS1123Subdomain(output.Name)) > 0 || output.NodeCount < 0 || output.ValkeyShards < 0 || output.ValkeyReplicas < 0 {
		return DraftIntent{}, errors.New("Bedrock returned invalid PlatformEnvironment fields")
	}
	return DraftIntent{Name: output.Name, Namespace: request.Namespace, ClassRef: request.ClassRef, Region: output.Region, NodeCount: output.NodeCount, ValkeyEnabled: output.ValkeyEnabled, ValkeyShards: output.ValkeyShards, ValkeyReplicas: output.ValkeyReplicas}, nil
}

var explicitNodeCount = regexp.MustCompile(`(?i)(\d+)\s*(?:nodes?|节点)`)
var asciiWords = regexp.MustCompile(`[a-zA-Z0-9]+`)

func inferNodeCount(description string) int32 {
	if match := explicitNodeCount.FindStringSubmatch(description); len(match) == 2 {
		var value int
		_, _ = fmt.Sscanf(match[1], "%d", &value)
		if value > 0 && value <= 100 {
			return int32(value)
		}
	}
	return 1
}
func slugName(description, className string) string {
	words := asciiWords.FindAllString(strings.ToLower(description), -1)
	skip := map[string]bool{"create": true, "build": true, "small": true, "a": true, "an": true, "the": true, "platform": true, "environment": true, "with": true, "for": true, "please": true, "new": true}
	parts := make([]string, 0, 3)
	for _, word := range words {
		if skip[word] {
			continue
		}
		parts = append(parts, word)
		if len(parts) == 3 {
			break
		}
	}
	slug := strings.Join(parts, "-")
	if slug == "" {
		class := strings.Trim(strings.ToLower(className), "-")
		if len(class) > 24 {
			class = class[:24]
		}
		slug = "env-" + class
	}
	if problems := validation.IsDNS1123Subdomain(slug); len(problems) > 0 {
		slug = "env-platform"
	}
	if len(slug) > 63 {
		slug = slug[:63]
	}
	return strings.Trim(slug, "-")
}
func localHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
