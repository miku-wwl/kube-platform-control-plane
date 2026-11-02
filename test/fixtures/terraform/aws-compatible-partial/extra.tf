# QA-only extension copied beside the pinned aws-compatible-basic fixture.
# Dependency order makes a second create or the final delete fail after one
# real S3 mutation. The source is committed before any Plan is created.
resource "aws_s3_bucket" "partial" {
  bucket        = "${var.bucket_name}-second"
  force_destroy = true
  depends_on    = [aws_s3_bucket.lifecycle]
}
