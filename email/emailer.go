package email

import (
	"context"
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
