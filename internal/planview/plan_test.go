package planview

import (
	"encoding/json"
	"testing"
)

func TestParseActionsMetadataAndDependencyGraph(t *testing.T) {
	input := []byte(`{
		"format_version":"1.2",
		"resource_changes":[
			{"address":"aws_s3_bucket.data","type":"aws_s3_bucket","provider_name":"registry.terraform.io/hashicorp/aws","change":{"actions":["create"],"before":null,"before_sensitive":false,"after":{"bucket":"platform-data","secret_key":"hidden","tags":{"team":"platform","api-token":"hidden"}},"after_sensitive":{"bucket":false,"secret_key":true,"tags":{"team":false,"api-token":true}}}},
			{"address":"aws_s3_bucket.logs","type":"aws_s3_bucket","change":{"actions":["delete","create"],"before":{"bucket":"old"},"after":{"bucket":"new"}}},
			{"address":"aws_instance.worker","type":"aws_instance","change":{"actions":["update"],"before":{"instance_type":"t3.small"},"after":{"instance_type":"t3.medium"}}}
		],
		"configuration":{"root_module":{"resources":[
			{"address":"aws_s3_bucket.data","expressions":{}},
			{"address":"aws_instance.worker","expressions":{"bucket":{"references":["aws_s3_bucket.data.arn"]}}}
		]}}
	}`)
	doc, err := Parse(input, "run-uid", "sha256:plan")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Summary.Create != 1 || doc.Summary.Update != 1 || doc.Summary.Replace != 1 {
		t.Fatalf("unexpected summary: %+v", doc.Summary)
	}
	if len(doc.Nodes) != 3 || len(doc.Edges) != 1 || doc.Edges[0] != (Edge{From: "aws_s3_bucket.data", To: "aws_instance.worker"}) {
		t.Fatalf("unexpected graph: %+v %+v", doc.Nodes, doc.Edges)
	}
	var bucket Node
	for _, node := range doc.Nodes {
		if node.Address == "aws_s3_bucket.data" {
			bucket = node
		}
	}
	if bucket.After["bucket"] != "platform-data" {
		t.Fatalf("safe bucket metadata missing: %#v", bucket.After)
	}
	if _, leaked := bucket.After["secret_key"]; leaked {
		t.Fatal("sensitive attribute leaked")
	}
	if _, leaked := bucket.After["api-token"]; leaked {
		t.Fatal("sensitive tag leaked")
	}
	if _, ok := bucket.After["tags"].(map[string]any)["team"]; !ok {
		t.Fatal("safe tag omitted")
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" {
		t.Fatal("empty document")
	}
}

func TestParseAcceptsWholeValueSensitiveMasks(t *testing.T) {
	input := []byte(`{
		"format_version":"1.2",
		"resource_changes":[{"address":"aws_s3_bucket.data","type":"aws_s3_bucket","change":{"actions":["update"],"before":{"bucket":"old"},"before_sensitive":true,"after":{"bucket":"new","region":"us-west-2"},"after_sensitive":false}}]
	}`)
	doc, err := Parse(input, "run-uid", "sha256:plan")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Nodes[0].Before != nil {
		t.Fatalf("whole-value sensitive before leaked: %#v", doc.Nodes[0].Before)
	}
	if doc.Nodes[0].After["bucket"] != "new" || doc.Nodes[0].After["region"] != "us-west-2" {
		t.Fatalf("false whole-value sensitive mask should preserve safe metadata: %#v", doc.Nodes[0].After)
	}
}

func TestParseRejectsUnsupportedMajorFormat(t *testing.T) {
	if _, err := Parse([]byte(`{"format_version":"2.0"}`), "uid", "digest"); err == nil {
		t.Fatal("expected unsupported major version error")
	}
}
