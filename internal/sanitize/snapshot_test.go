package sanitize

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"buyersworkbench/internal/domain"
)

func TestProjectionExcludesPrivateCanaries(t *testing.T) {
	record := domain.SeedRecords()[0]
	record.Origin = "live_private"
	canaries := []string{"canary-account-582", "canary-order-683", "canary-email@example.test", "canary-part-286", "canary-private-url-723", "canary-source-label-581"}
	record.SourceKey = canaries[0]
	record.SourceOrderID = canaries[1]
	record.Description = canaries[2]
	record.SupplierPart = canaries[3]
	record.SupplierAlias = canaries[4]
	record.ArrivalLabel = canaries[5]
	now := time.Now().UTC()
	snapshot, err := Project("independent-public-id", record, now, now, Approval{"Generic computer accessory", true, true, true})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(snapshot)
	for _, secret := range canaries {
		if strings.Contains(string(data), secret) {
			t.Fatal("private canary entered projection")
		}
	}
	if snapshot.Origin != "sanitized_snapshot" || snapshot.ID == record.SourceKey || snapshot.SanitizerVersion != Version {
		t.Fatal("snapshot lacks independent provenance")
	}
}
func TestProjectionRequiresExplicitFieldsAndSafeDescription(t *testing.T) {
	record := domain.SeedRecords()[0]
	record.Origin = "live_private"
	now := time.Now()
	result, err := Project("new-id", record, now, now, Approval{Description: "Generic component"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Ordered != nil || result.Status != "" || result.ArrivalDate != "" {
		t.Fatal("unapproved fields included")
	}
	for _, name := range []string{"", "private@example.test", "https://private.example.test/order", "user\nname"} {
		if _, err := Project("id", record, now, now, Approval{Description: name}); err == nil {
			t.Fatal("unsafe description accepted")
		}
	}
	record.Origin = "fictional"
	if _, err := Project("id", record, now, now, Approval{Description: "Sample"}); err == nil {
		t.Fatal("fictional data relabeled as private snapshot")
	}
}
