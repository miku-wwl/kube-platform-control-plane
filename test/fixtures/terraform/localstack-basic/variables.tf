variable "region" {
  type    = string
  default = "us-east-1"
}

variable "s3_endpoint" {
  type    = string
  default = "http://host.docker.internal:4566"
}

variable "bucket_name" {
  type    = string
  default = "pcp-e2e-lifecycle-bucket"
}

variable "target_provider" {
  type    = string
  default = "kind"
}

variable "target_account_id" {
  type    = string
  default = "local"
}

variable "target_cluster_name" {
  type    = string
  default = "kind-pcp-target-local"
}

variable "target_endpoint" {
  type    = string
  default = "kubeconfig:kind-pcp-target-local"
}
