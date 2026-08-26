package identity

import (
	"maps"
	"time"

	"github.com/primandproper/platform-go/v13/database/querygen"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/identity/internal/queries"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// What each rendered statement binds, keyed by the argument name the statement
// names — never by position.
//
// The statements themselves are in statements.go, and this is deliberately the
// other file. A Bound holds SQL and an ordered list of argument names; these
// functions hold the values those names stand for, and neither half has to know
// the other's order. That is the property the hand-numbered builders these
// replaced could not have: there, a column and its placeholder were two edits in
// two places, and getting the second one wrong produced a statement that ran.
//
// The maps are built per call rather than shared, because [statements.listValues]
// copies the window into what it is given and a package-level map would carry one
// read's arguments into the next one's.

// keyed is the argument map a statement keyed on one row takes: the id, and the
// scope every statement here is also keyed on.
func keyed(scope tenancy.Scope, id string) map[string]any {
	return map[string]any{
		querygen.IDColumn:   id,
		queries.ScopeColumn: scope,
	}
}

// keyedScope is the match map a list statement keyed on nothing but the scope
// takes. It is a function rather than a package-level map because listValues
// copies into what it is given and a shared map would accumulate one read's
// arguments into the next one's.
func keyedScope(scope tenancy.Scope) map[string]any {
	return map[string]any{queries.ScopeColumn: scope}
}

// listValues assembles what a rendered list statement binds: the filter window
// under the argument names querygen emits, and the match columns beside it.
//
// The window goes through Generator.BindFilter rather than being written out
// here, because two of its seven values are not what the Go field holds —
// SQLite compares timestamps as text and MySQL's LIMIT cannot coalesce — and a
// second copy of that mapping would bind under names no statement mentions,
// which filters nothing and looks exactly like a filter nobody set.
func (s *statements) listValues(filter *filtering.QueryFilter, matches map[string]any) map[string]any {
	values := make(map[string]any, len(matches)+7)
	maps.Copy(values, matches)

	s.generator.BindFilter(values, filter)

	return values
}

// userValues is every column the user create binds, keyed by the column it
// binds to.
//
// created_at is absent, and its absence is the schema's — see
// identity/internal/queries and the DEFAULT in identity/migrations. The four
// database-owned columns are the ones a caller must not supply, because a row
// whose creation time disagrees with its id is a row the cursor walk and the
// filter window order differently.
func userValues(u *User) map[string]any {
	return map[string]any{
		"id":                                 u.ID,
		queries.ScopeColumn:                  u.Scope,
		"username":                           u.Username,
		"email_address":                      u.EmailAddress,
		"first_name":                         u.FirstName,
		"last_name":                          u.LastName,
		"hashed_password":                    u.HashedPassword,
		"requires_password_change":           u.RequiresPasswordChange,
		"password_last_changed_at":           u.PasswordLastChangedAt,
		"two_factor_secret":                  u.TwoFactorSecret,
		"two_factor_secret_verified_at":      u.TwoFactorSecretVerifiedAt,
		queries.EmailAddressVerifiedAtColumn: u.EmailAddressVerifiedAt,
		"email_address_verification_token":   u.EmailAddressVerificationToken,
		"account_status":                     u.AccountStatus.String(),
		"account_status_explanation":         u.AccountStatusExplanation,
		"last_accepted_terms_of_service":     u.LastAcceptedTermsOfService,
		"last_accepted_privacy_policy":       u.LastAcceptedPrivacyPolicy,
	}
}

// userUpdateValues is what the profile update assigns, plus the row it is keyed
// on.
//
// verifiedAt is the caller's decision rather than the user's own field: moving
// an address has to clear the proof that went with it, and only the store knows
// what the stored address was. See SQLStore.UpdateUser.
func userUpdateValues(u *User, verifiedAt *time.Time) map[string]any {
	values := keyed(u.Scope, u.ID)

	values["username"] = u.Username
	values["email_address"] = u.EmailAddress
	values["first_name"] = u.FirstName
	values["last_name"] = u.LastName
	values[queries.EmailAddressVerifiedAtColumn] = verifiedAt

	return values
}

// accountValues is every column the account create binds.
func accountValues(a *Account) map[string]any {
	return map[string]any{
		"id":                              a.ID,
		queries.ScopeColumn:               a.Scope,
		"name":                            a.Name,
		"owner_user_id":                   a.OwnerUserID,
		"billing_status":                  a.BillingStatus.String(),
		"subscription_plan_id":            a.SubscriptionPlanID,
		"payment_processor_customer_id":   a.PaymentProcessorCustomerID,
		"last_payment_provider_synced_at": a.LastPaymentProviderSyncedAt,
		"address_line1":                   a.BillingAddress.Line1,
		"address_line2":                   a.BillingAddress.Line2,
		"address_city":                    a.BillingAddress.City,
		"address_state":                   a.BillingAddress.State,
		"address_postal_code":             a.BillingAddress.PostalCode,
		"address_country":                 a.BillingAddress.Country,
		"address_phone":                   a.BillingAddress.Phone,
		"time_zone":                       a.TimeZone,
	}
}

// accountUpdateValues is the name and the address, plus the row it is keyed on.
// Neither the owner nor any billing column is here — each has a write of its
// own, and an update that assigned them would let a profile save undo a
// processor webhook.
func accountUpdateValues(a *Account) map[string]any {
	values := keyed(a.Scope, a.ID)

	values["name"] = a.Name
	values["address_line1"] = a.BillingAddress.Line1
	values["address_line2"] = a.BillingAddress.Line2
	values["address_city"] = a.BillingAddress.City
	values["address_state"] = a.BillingAddress.State
	values["address_postal_code"] = a.BillingAddress.PostalCode
	values["address_country"] = a.BillingAddress.Country
	values["address_phone"] = a.BillingAddress.Phone
	values["time_zone"] = a.TimeZone

	return values
}

// invitationValues is every column the invitation create binds.
func invitationValues(i *Invitation) map[string]any {
	return map[string]any{
		"id":                   i.ID,
		queries.ScopeColumn:    i.Scope,
		"belongs_to_account":   i.BelongsToAccount,
		"from_user":            i.FromUser,
		"to_email":             i.ToEmail,
		"to_name":              i.ToName,
		"to_user":              i.ToUser,
		"token":                i.Token,
		invitationStatusColumn: i.Status.String(),
		"note":                 i.Note,
		"expires_at":           i.ExpiresAt.UTC(),
	}
}
