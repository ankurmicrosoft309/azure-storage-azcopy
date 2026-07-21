# Feasibility of metrics

- NIC speed - best effort
- Network route - Private Link or Public? - use traceroute?
- Size of Blob containers and S3/GCS buckets

# Require Effort but feasible
- CPU & Memory usage - need to track self use throughout the job and identify peaks and average
- Subscription Id, Tenant Id, Storage account region - get this server side from storage account id/name
- Retry counts
    - AzCopy retries at several layers:

    Azure SDK HTTP-policy retries

    Every retry passes through the per-retry policies assembled in mgr-JobPartMgr.go:113. This includes throttling, transient service failures, and network errors handled by azcore.

    Response-body retries

    Downloads use SDK RetryReader implementations after the initial response headers arrive. Examples are in downloader-blob.go:177 and downloader-azureFiles.go:138.

    These are not necessarily identifiable as retries by the normal HTTP-operation policy. There is already a TODO noting that Azure Files does not attach the retry notification context.

    Authentication retries

    destReauthPolicy.go performs an internal reauthentication/retry loop.

    Manual application retries

    AzCopy has explicit loops for operations such as symlink creation, local interrupted reads, and some traversal behaviors. These do not all pass through one shared counter.

    Resume

    Resume is a new attempt and should not be counted as an HTTP retry.

 - Finalization duration - after data transfer completes, how long does it take for everything to wrap up (including metrics sending)

# Not reliably supportable with % JobID sampling:

Exact jobs or bytes per customer.
Exact “last active” date.
First-job success and time to first success.
Percentage of customers running a second job.
Complete customer histories or precise low-volume subscription trends.
Accurate rare-customer/top-mover rankings

Support-case, self-service-resolution, mitigation, escalation, and repeat-contact metrics.



- Sync with Raj, and updated docs
- Update ADOs