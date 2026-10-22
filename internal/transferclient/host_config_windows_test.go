//go:build windows

package transferclient

import (
	"strings"
	"testing"
)

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

func TestWindowsNativeReleaseUsesJournalIdentity(t *testing.T) {
	for _, id := range []string{"delivery-1", "CON", "a", "A"} {
		journal := strings.TrimSuffix(queueName(id+".journal"), ".journal")
		release := strings.TrimSuffix(queueName(id+".release"), ".release")
		if journal != release || release == id {
			t.Fatal("compaction witness identity differs")
		}
	}
}

func TestWindowsNativeProducerMultiple(t *testing.T) {
	TestProducerMultiplePartialRestartAndDeniedFile(t)
}

func TestWindowsNativeProducerFeedback(t *testing.T) {
	TestProducerFeedbackScopeAndUnchangedBlock(t)
	TestProducerFeedbackDeniedFirstAndMiddle(t)
}
