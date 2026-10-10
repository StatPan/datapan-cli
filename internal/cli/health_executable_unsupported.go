//go:build !linux

package cli

import "errors"

var errHealthRunningImageAttestationUnavailable = errors.New("running-image attestation requires Linux /proc/self/exe")

// Plan execution requires a digest of the actual running image, which this
// implementation currently attests through Linux /proc/self/exe. Keep the
// command fail-closed on other platforms rather than hashing a replaceable path.
func healthExecutableSHA256() (string, error) {
	return "", errHealthRunningImageAttestationUnavailable
}
