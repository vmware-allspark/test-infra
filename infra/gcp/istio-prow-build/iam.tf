resource "google_service_account" "external_secrets_private" {
  account_id   = "external-secrets-private"
  description  = "Kubernetes External Secrets controller for private cluster"
  display_name = "external-secrets-private"
  project      = "istio-prow-build"
}
# WARNING: the description here is false. I have no clue what this is used for TBH
# The default prowjob SA is istio-prow-test-job-default@istio-testing.iam.gserviceaccount.com
resource "google_service_account" "istio_prow_jobs" {
  account_id   = "istio-prow-jobs"
  description  = "The default service account that will be used for Prow job workloads."
  display_name = "istio-prow-jobs"
  project      = "istio-prow-build"
}
resource "google_service_account" "kubernetes_external_secrets_sa" {
  account_id   = "kubernetes-external-secrets-sa"
  description  = "Service account used by external secrets controller"
  display_name = "kubernetes-external-secrets-sa"
  project      = "istio-prow-build"
}
resource "google_service_account" "prow_internal_storage" {
  account_id   = "prow-internal-storage"
  description  = "Internal Prow SA for istio-private-build GCS. "
  display_name = "Prow Internal Storage"
  project      = "istio-prow-build"
}
