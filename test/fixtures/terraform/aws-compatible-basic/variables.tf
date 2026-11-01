variable "region" {
  type    = string
  default = "us-east-1"
}

variable "s3_endpoint" {
  type = string
}

variable "sts_endpoint" {
  type = string
}

variable "bucket_name" {
  type = string
}

variable "target_provider" {
  type    = string
  default = "kubernetes"
}

variable "target_account_id" {
  type    = string
  default = "local"
}

variable "target_cluster_name" {
  type = string
}

variable "target_endpoint" {
  type = string
}

variable "target_region" {
  type    = string
  default = "local"
}

variable "target_auth_mode" {
  type    = string
  default = "kubeconfig-context"
}

variable "target_incarnation_id" {
  type = string
}

variable "execution_role_arn" {
  type    = string
  default = ""
}
