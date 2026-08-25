package grpc

import (
	"reflect"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/filtering/filteringpb"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The schema and the struct are two descriptions of one type, and this is what
// keeps them the same description. A field added to filtering.QueryFilter and
// not to the .proto is a field a gRPC client cannot send; a field added to the
// .proto and not to the struct is one a server cannot read. Both are silent:
// everything still compiles, the converters still convert, and the value simply
// does not arrive.
//
// The two are matched on their JSON names rather than on the Go identifiers,
// because that is a name both descriptions already carry — protobuf derives
// json_name from the field name, and the struct carries a `json` tag for the
// HTTP transport — so the check is between two things that exist for their own
// reasons rather than against a list kept here.
//
// It reflects over the Go struct rather than reading a list, so it holds for
// fields nobody thought to add to this file.

// jsonNames returns the JSON name of every exported field on a struct type.
//
// The blank fields carrying JSON Schema keywords are unexported, so they are
// skipped along with anything else the encoder would not write.
func jsonNames(t *testing.T, rt reflect.Type) map[string]string {
	t.Helper()

	names := map[string]string{}

	for field := range rt.Fields() {
		if !field.IsExported() {
			continue
		}

		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}

		names[name] = field.Name
	}

	must.MapNotEmpty(t, names)

	return names
}

// protoJSONNames returns the JSON name of every field on a message.
func protoJSONNames(t *testing.T, md protoreflect.MessageDescriptor) map[string]string {
	t.Helper()

	names := map[string]string{}

	fields := md.Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		names[field.JSONName()] = string(field.Name())
	}

	must.MapNotEmpty(t, names)

	return names
}

func assertConformance(t *testing.T, rt reflect.Type, md protoreflect.MessageDescriptor) {
	t.Helper()

	goFields := jsonNames(t, rt)
	protoFields := protoJSONNames(t, md)

	for jsonName, goName := range goFields {
		if _, ok := protoFields[jsonName]; !ok {
			t.Errorf("%s.%s (json %q) has no field in %s; add it to the .proto and regenerate with `make proto format`",
				rt.Name(), goName, jsonName, md.FullName())
		}
	}

	for jsonName, protoName := range protoFields {
		if _, ok := goFields[jsonName]; !ok {
			t.Errorf("%s.%s (json %q) has no field on %s; the schema describes something the struct cannot hold",
				md.FullName(), protoName, jsonName, rt.Name())
		}
	}
}

func TestSchemaConformance(T *testing.T) {
	T.Parallel()

	T.Run("QueryFilter", func(t *testing.T) {
		t.Parallel()

		assertConformance(t,
			reflect.TypeFor[filtering.QueryFilter](),
			(&filteringpb.QueryFilter{}).ProtoReflect().Descriptor(),
		)
	})

	T.Run("Pagination", func(t *testing.T) {
		t.Parallel()

		assertConformance(t,
			reflect.TypeFor[filtering.Pagination](),
			(&filteringpb.Pagination{}).ProtoReflect().Descriptor(),
		)
	})
}

// TestFieldNames keeps the field names the error messages use from naming a
// field the schema does not have. They are literals in converters.go, and a
// renamed proto field would otherwise leave them pointing at nothing.
func TestFieldNames(T *testing.T) {
	T.Parallel()

	T.Run("QueryFilter", func(t *testing.T) {
		t.Parallel()

		fields := (&filteringpb.QueryFilter{}).ProtoReflect().Descriptor().Fields()

		for _, name := range []string{
			fieldCreatedAfter,
			fieldCreatedBefore,
			fieldUpdatedAfter,
			fieldUpdatedBefore,
		} {
			test.NotNil(t, fields.ByName(protoreflect.Name(name)), test.Sprintf("no field named %q", name))
		}
	})

	T.Run("Pagination", func(t *testing.T) {
		t.Parallel()

		fields := (&filteringpb.Pagination{}).ProtoReflect().Descriptor().Fields()

		test.NotNil(t, fields.ByName(protoreflect.Name(fieldAppliedFilter)),
			test.Sprintf("no field named %q", fieldAppliedFilter))
	})
}

// TestPackageName pins the proto package. Proto package names are global and
// generated clients are nominal in it, so a rename is breaking in every
// language a consumer generates into, all at once — it is a coordinated bump
// across their trees rather than a change to this file.
func TestPackageName(T *testing.T) {
	T.Parallel()

	test.EqOp(T, protoreflect.FullName("primandproper.platform.filtering.v1"),
		(&filteringpb.QueryFilter{}).ProtoReflect().Descriptor().ParentFile().Package())
}

// TestFieldNumbers pins the field numbers, which are the compatibility promise
// this schema makes. Renumbering a field is not a change to a wire format, it
// is a different message that happens to compile.
func TestFieldNumbers(T *testing.T) {
	T.Parallel()

	T.Run("QueryFilter", func(t *testing.T) {
		t.Parallel()

		assertFieldNumbers(t, (&filteringpb.QueryFilter{}).ProtoReflect().Descriptor(), map[string]int32{
			"sort_by":           1,
			"created_after":     2,
			"created_before":    3,
			"updated_after":     4,
			"updated_before":    5,
			"max_response_size": 6,
			"include_archived":  7,
			"cursor":            8,
		})
	})

	T.Run("Pagination", func(t *testing.T) {
		t.Parallel()

		assertFieldNumbers(t, (&filteringpb.Pagination{}).ProtoReflect().Descriptor(), map[string]int32{
			"applied_query_filter": 1,
			"cursor":               2,
			"previous_cursor":      3,
			"filtered_count":       4,
			"total_count":          5,
			"max_response_size":    6,
			"counts_known":         7,
		})
	})
}

func assertFieldNumbers(t *testing.T, md protoreflect.MessageDescriptor, expected map[string]int32) {
	t.Helper()

	fields := md.Fields()

	test.EqOp(t, len(expected), fields.Len())

	for name, number := range expected {
		field := fields.ByName(protoreflect.Name(name))
		if field == nil {
			t.Errorf("%s has no field named %q", md.FullName(), name)

			continue
		}

		test.EqOp(t, number, int32(field.Number()), test.Sprintf("field %q moved", name))
	}
}
