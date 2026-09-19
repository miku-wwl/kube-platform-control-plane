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
