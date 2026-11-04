package eventmapper

import (
	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
	"github.com/luigimenezes13/financial-manager/internal/platform/outbox"
)

const firstVersion = 1

func All() []outbox.EventMapper {
	return []outbox.EventMapper{
		outbox.MapperFor(identity.EventTypeRegistered, firstVersion, userRegistered),
	}
}

type userRegisteredPayload struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	Provider string `json:"provider"`
}

func userRegistered(event identity.Registered) userRegisteredPayload {
	userID := event.UserID()
	email := event.Email()

	return userRegisteredPayload{
		UserID:   userID.String(),
		Email:    email.String(),
		Provider: event.Provider(),
	}
}
