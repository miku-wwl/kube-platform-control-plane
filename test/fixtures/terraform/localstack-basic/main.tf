terraform {
  required_version = "= 1.14.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "= 5.100.0"
    }
  }

  backend "s3" {}
}

provider "aws" {
  region                      = var.region
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_region_validation      = true
  skip_requesting_account_id  = true
  skip_metadata_api_check     = true
  s3_use_path_style           = true

  endpoints {
    s3 = var.s3_endpoint
  }
}

resource "aws_s3_bucket" "lifecycle" {
  bucket        = var.bucket_name
  force_destroy = true
}

output "bucket_name" {
  value = aws_s3_bucket.lifecycle.bucket
}
