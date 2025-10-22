package exporter

import (
	"math/rand"
	"reflect"
	"testing"

	fuzz "github.com/google/gofuzz"
)

var deepCopyTypes = []reflect.Type{
	reflect.TypeOf(DecidedTrace{}),
	reflect.TypeOf(RoundTrace{}),
	reflect.TypeOf(RoundChangeTrace{}),
	reflect.TypeOf(ProposalTrace{}),
	reflect.TypeOf(QBFTTrace{}),
	reflect.TypeOf(CommitteeDutyTrace{}),
	reflect.TypeOf(SignerData{}),
	reflect.TypeOf(ValidatorDutyTrace{}),
	reflect.TypeOf(PartialSigTrace{}),
}

func TestDeepCopy(t *testing.T) {
	f := fuzz.New().NilChance(0).NumElements(0, 5).RandSource(rand.NewSource(0))
	for _, typ := range deepCopyTypes {
		t.Run(typ.Name(), func(t *testing.T) {
			orig := reflect.New(typ)
			f.Fuzz(orig.Interface())

			copyVal := orig.MethodByName("DeepCopy").Call(nil)[0]

			if orig.Pointer() == copyVal.Pointer() {
				t.Fatalf("%s DeepCopy returned same pointer", typ.Name())
			}

			if !reflect.DeepEqual(orig.Interface(), copyVal.Interface()) {
				t.Fatalf("%s DeepCopy mismatch", typ.Name())
			}

			nilVal := reflect.Zero(reflect.PointerTo(typ))
			nilCopy := nilVal.MethodByName("DeepCopy").Call(nil)[0]
			if !nilCopy.IsNil() {
				t.Fatalf("%s DeepCopy nil receiver expected nil", typ.Name())
			}
		})
	}
}
