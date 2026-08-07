# data.go.kr renewal: read-only compatibility audit

Evidence class: observed, bounded local-browser observation

Observation date: 2026-08-07 (Asia/Seoul)

## Scope and redaction

An authenticated local browser was used only to inspect the renewed
data.go.kr account/dashboard surface. No application form was submitted, no
provider API was called, and no portal write was attempted. Account identity,
service-key values, cookies, dashboard counts, request URLs with query values,
and page source are deliberately absent from this record.

The observation confirms that the portal owns account-level application and
credential workflows. It does **not** establish a deterministic per-service
application-form contract. In particular, it does not establish current form
fields, consent semantics, confirmation/result navigation, or duplicate-request
behavior. Those omissions are why CLI submission remains fail-closed.

## Bounded receipt

```json
{
  "schema_version": "datapan.data-go-kr.portal-compatibility.v1",
  "evidence_class": "observed",
  "provider": "data.go.kr",
  "state": "unknown",
  "mode": "read_only",
  "submission": "blocked_pending_renewed_form_evidence",
  "next_action": "capture_reviewed_per_service_dom_and_url_contract_before_reenable",
  "evidence": [
    "local_authenticated_dashboard_observed",
    "per_service_form_semantics_not_captured"
  ],
  "redaction": {
    "account_identity": false,
    "credential_values": false,
    "cookies": false,
    "query_values": false,
    "page_source": false
  }
}
```

`false` means the field is not present in this receipt; it is not a statement
about whether such material exists in the portal.

## Release and rollback boundary

This is not a portal release and creates no provider-side state. The CLI change
is released only through the normal tagged-release workflow after PR review.
Before release, rollback is closing or reverting the PR. After release, rollback
is a follow-up patch that retains fail-closed behavior until a replacement
contract is independently reviewed.
