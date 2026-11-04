package eventmapper_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/eventmapper"
	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
	"github.com/luigimenezes13/financial-manager/internal/platform/outbox"
)

const userUUID = "0193a3b0-0000-7000-8000-000000000010"

func TestIdentityEventMappers(t *testing.T) {
	t.Run("should map a registered user without leaking the provider subject", func(t *testing.T) {
		userID, err := identity.UserIDFromUUID(uuid.MustParse(userUUID))
		if err != nil {
			t.Fatalf("user id: %v", err)
		}
		email, err := identity.NewEmail("ana@example.com")
		if err != nil {
			t.Fatalf("email: %v", err)
		}
		external, err := identity.NewExternalIdentity("google", "google-subject-123")
		if err != nil {
			t.Fatalf("external identity: %v", err)
		}
		mappers, err := outbox.NewMappers(eventmapper.All()...)
		if err != nil {
			t.Fatalf("new mappers: %v", err)
		}

		envelope, err := mappers.Seal(identity.NewRegistered(userID, email, external))
		if err != nil {
			t.Fatalf("seal: %v", err)
		}

		if envelope.Name != "identity.user.registered" || envelope.Version != 1 {
			t.Errorf("name/version = %q/%d", envelope.Name, envelope.Version)
		}
		want := `{"user_id":"` + userUUID + `","email":"ana@example.com","provider":"google"}`
		if string(envelope.Payload) != want {
			t.Errorf("payload\n got: %s\nwant: %s", envelope.Payload, want)
		}
		if strings.Contains(string(envelope.Payload), "google-subject-123") {
			t.Error("payload leaks the provider subject")
		}
	})
}
