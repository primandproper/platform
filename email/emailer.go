package email

import (
	"context"
	"net/mail"
	"strings"
)

type (
	// OutboundEmailMessage is a collection of fields that are useful for sending
	// emails.
	//
	// It carries only what an SMTP-or-API emailer needs to address and render one
	// message. Application identifiers — which user this concerns, which test
	// produced it — belong to the application's own envelope, not to the
	// platform's transport type.
	OutboundEmailMessage struct {
		ToAddress   string `json:"toAddress"`
		ToName      string `json:"toName"`
		FromAddress string `json:"fromAddress"`
		FromName    string `json:"fromName"`
		Subject     string `json:"subject"`
		HTMLContent string `json:"htmlContent"`
	}

	// Emailer represents a service that can send emails.
	Emailer interface {
		SendEmail(ctx context.Context, details *OutboundEmailMessage) error
	}
)

// FormatAddress renders a display name and address as one RFC 5322 mailbox,
// quoting and escaping the name. An empty name yields the bare address.
//
// It is a shared function rather than a Sprintf at each provider — as three of
// them used to have — because the escaping is load-bearing. A comma in an
// attacker-influenced display name injects extra recipients wherever the
// provider accepts a comma-separated list, which several of them do; net/mail's
// own rendering is what quotes it.
//
// Four providers carried a byte-identical copy of this and only one carried the
// reason for it, which is the argument for one home: the copy most likely to be
// deleted by somebody tidying is one of the three that looks like a formatting
// convenience.
func FormatAddress(name, address string) string {
	if strings.TrimSpace(name) == "" {
		return address
	}

	return (&mail.Address{Name: name, Address: address}).String()
}
