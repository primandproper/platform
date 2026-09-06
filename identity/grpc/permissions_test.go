package grpc_test

import (
	"slices"
	"testing"

	authzgrpc "github.com/primandproper/platform-go/v14/authorization/grpc"
	identitygrpc "github.com/primandproper/platform-go/v14/identity/grpc"
	"github.com/primandproper/platform-go/v14/identity/identitypb"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// serviceMethods is every RPC the generated service descriptor declares, as the
// full method names the interceptor matches on.
//
// It is read off the descriptor rather than written down, which is the whole
// point: a list here would be a third place to forget an RPC, and the two tests
// below exist because the first two are easy to forget.
func serviceMethods() []string {
	prefix := "/" + identitypb.IdentityService_ServiceDesc.ServiceName + "/"

	out := make([]string, 0, len(identitypb.IdentityService_ServiceDesc.Methods))
	for _, m := range identitypb.IdentityService_ServiceDesc.Methods {
		out = append(out, prefix+m.MethodName)
	}

	return out
}

// TestEveryMethodHasADecision is the reason this file exists. An RPC added to
// the service later and named in neither Permissions nor SelfServiceMethods has
// had no decision made about it, and the way a consumer would find out is
// authorization/grpc denying it in production — correctly, and for a reason
// nobody would connect to this package.
func TestEveryMethodHasADecision(T *testing.T) {
	T.Parallel()

	methods := serviceMethods()
	must.SliceNotEmpty(T, methods, must.Sprint("no methods on the service descriptor, so this asserted nothing"))

	permissions := identitygrpc.Permissions()
	selfService := identitygrpc.SelfServiceMethods()

	for _, method := range methods {
		T.Run(method, func(t *testing.T) {
			t.Parallel()

			_, permissioned := permissions[method]
			self := slices.Contains(selfService, method)

			test.True(t, permissioned || self, test.Sprintf(
				"%s is in neither Permissions nor SelfServiceMethods, so nothing says who may call it", method))
			test.False(t, permissioned && self, test.Sprintf(
				"%s is in both Permissions and SelfServiceMethods, and authorization/grpc refuses a method declared twice", method))
		})
	}
}

// TestNoDecisionOutlivesItsMethod is the other direction, and the one a rename
// breaks. An entry naming an RPC the service no longer has reads exactly like a
// live one, and the permission it names then guards nothing.
func TestNoDecisionOutlivesItsMethod(T *testing.T) {
	T.Parallel()

	methods := serviceMethods()

	for method := range identitygrpc.Permissions() {
		test.True(T, slices.Contains(methods, method), test.Sprintf(
			"Permissions names %s, which is not an RPC on this service", method))
	}

	for _, method := range identitygrpc.SelfServiceMethods() {
		test.True(T, slices.Contains(methods, method), test.Sprintf(
			"SelfServiceMethods names %s, which is not an RPC on this service", method))
	}
}

// TestNoPermissionIsEmpty covers what authorization/grpc's builder refuses:
// an empty requirement list is ErrNoPermissionsRequired, and an empty
// permission string is ErrEmptyPermission. Both are easy to write and neither
// is visible until Build is called.
func TestNoPermissionIsEmpty(T *testing.T) {
	T.Parallel()

	for method, perms := range identitygrpc.Permissions() {
		test.SliceNotEmpty(T, perms, test.Sprintf("%s requires an empty permission set", method))

		for _, p := range perms {
			test.NotEqOp(T, "", p, test.Sprintf("%s requires an empty permission", method))
		}
	}
}

// TestTheFragmentComposes is the acceptance test for what this map is for: a
// consumer hands it to authorization/grpc and gets a Requirements out. It is
// asserted here rather than left to a consumer to discover, because every
// failure Build reports — a duplicate method, an empty requirement — is one this
// package could have committed.
func TestTheFragmentComposes(T *testing.T) {
	T.Parallel()

	builder := authzgrpc.NewRequirements().RequireAll(identitygrpc.Permissions())
	for _, method := range identitygrpc.SelfServiceMethods() {
		builder.Public(method)
	}

	reqs, err := builder.Build()
	must.NoError(T, err)
	must.NotNil(T, reqs)

	// Every RPC is declared, which is what keeps the enforcer's fail-closed rule
	// from denying a method this package meant to allow.
	test.SliceLen(T, len(serviceMethods()), reqs.Methods())
}

// TestSelfServiceMethodsTakeNoSubject is the property that makes a self-service
// method safe without a permission: it has no way to name somebody else.
//
// It is asserted against the request messages rather than against prose, so an
// RPC that grows a target_user_id and stays on the self-service list fails here.
func TestSelfServiceMethodsTakeNoSubject(T *testing.T) {
	T.Parallel()

	// The fields a request would use to name a subject other than the caller.
	subjectFields := []string{"user_id", "email_address", "username"}

	for _, method := range identitygrpc.SelfServiceMethods() {
		T.Run(method, func(t *testing.T) {
			t.Parallel()

			desc := requestDescriptorFor(t, method)
			fields := desc.Fields()

			for i := range fields.Len() {
				name := string(fields.Get(i).Name())
				test.False(t, slices.Contains(subjectFields, name), test.Sprintf(
					"%s is self-service and its request carries %q, which names somebody other than the caller",
					method, name))
			}
		})
	}
}
