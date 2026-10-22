//go:build windows

package transferclient

import "testing"

func configFixtureRoot(t *testing.T) string { t.Helper(); return nativeFixtureRoot(t, false) }

func TestWindowsNativeConfiguredHost(t *testing.T) { TestConfiguredHostSecretsPathsAndRecovery(t) }

func TestWindowsNativeProducerClosedManifest(t *testing.T) {
	TestProducerClosedManifestRestartAndLostAssociation(t)
}

func TestWindowsNativeProducerMissingSource(t *testing.T) {
	TestProducerCollectMissingSourceStillAdvances(t)
}

func TestWindowsNativeQueueReleaseCapacity(t *testing.T) {
	TestQueueReleaseConfirmedFreesJobAndByteCapacity(t)
}

func TestWindowsNativeQueueReleaseUnknown(t *testing.T) {
	TestQueueReleaseNeverTreatsUnknownOrCancelAsDelivered(t)
}
func TestWindowsNativeQueueReleaseProcessCrash(t *testing.T) { TestQueueReleaseProcessCrash(t) }
