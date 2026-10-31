# Fixed AWS-compatible lifecycle fixture

This committed HCL module replaces the `localstack-basic` path. Terraform remains
pinned to 1.14.0, AWS provider to 5.100.0, and the original dependency lockfile is
retained. No source generator or emulator bootstrap is involved.

The module creates one explicitly named S3 bucket and outputs an allowlisted
connection to an already supplied Kubernetes target. It does not create a cluster
or scale nodes. Its capacity-only environment update therefore produces NoChange.

Required inputs: `s3_endpoint`, `sts_endpoint`, `bucket_name`,
`target_cluster_name` and `target_endpoint`. Use an explicit kubeconfig target
context registered in the controller, with endpoint `kubeconfig:<context>`.
`target_region=local` is separate from the emulated AWS region (`us-east-1`).
Defaults use `kubernetes` / `kubeconfig-context`; legacy existing classes and
retained bundles keep their own frozen values.

Credentials are supplied externally to the execution workload; there are no
inline test keys. Provider endpoints do not configure the S3 backend: the class
must reference its own credential-free backend ConfigMap with explicit supplied
endpoint, region, bucket, state key and `use_lockfile=true`. Backend snapshots and
source revisions remain immutable evidence. MiniStack S3/STS/backend compatibility
has not been verified.

Only Go Operator-owned Jobs may perform the test lifecycle against newly owned
emulator resources after exact approval. Do not manually Plan/Apply/Destroy against
pre-existing state. `force_destroy=true` belongs only to this disposable fixture;
it does not authorize deleting external buckets or bypassing finalizers.
