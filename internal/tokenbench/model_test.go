package tokenbench

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/goxide-lang/syntax/token"
)

func TestFlatLayoutMatchesToken(t *testing.T) {
	actual, baseline := reflect.TypeFor[token.Token](), reflect.TypeFor[flatToken]()
	checkStructLayout(t, actual, baseline, map[string]bool{"children": true, "input": true})

	actualChildren, _ := actual.FieldByName("children")
	baselineChildren, _ := baseline.FieldByName("children")
	checkStructLayout(t, actualChildren.Type, baselineChildren.Type, map[string]bool{"tokens": true})
	actualSlice, baselineSlice := actualChildren.Type.Field(0).Type, baselineChildren.Type.Field(0).Type
	if actualSlice.Kind() != reflect.Slice || baselineSlice.Kind() != reflect.Slice ||
		actualSlice.Elem() != actual || baselineSlice.Elem() != baseline {
		t.Fatal("recursive token stream layout differs")
	}

	actualInput, _ := actual.FieldByName("input")
	baselineInput, _ := baseline.FieldByName("input")
	if actualInput.Type.Kind() != reflect.Pointer || baselineInput.Type.Kind() != reflect.Pointer {
		t.Fatal("input references must both be pointers")
	}
	checkStructLayout(t, actualInput.Type.Elem(), baselineInput.Type.Elem(), nil)

	t.Logf("headers: token.Token=%d flatToken=%d Candidate=%d bytes", unsafe.Sizeof(token.Token{}), unsafe.Sizeof(flatToken{}), unsafe.Sizeof(Candidate{}))
	t.Logf("payload sizes: Ident=%d Keyword=%d Literal=%d Punct=%d Semicolon=%d Group=%d Fragment=%d bytes",
		unsafe.Sizeof(Ident{}), unsafe.Sizeof(Keyword{}), unsafe.Sizeof(Literal{}),
		unsafe.Sizeof(Punct{}), unsafe.Sizeof(Semicolon{}), unsafe.Sizeof(Group{}), unsafe.Sizeof(Fragment{}))
}

func checkStructLayout(t *testing.T, actual, baseline reflect.Type, recursive map[string]bool) {
	t.Helper()
	if actual.Kind() != reflect.Struct || baseline.Kind() != reflect.Struct ||
		actual.Size() != baseline.Size() || actual.Align() != baseline.Align() ||
		actual.NumField() != baseline.NumField() {
		t.Fatalf("layout mismatch: %s size=%d align=%d fields=%d; %s size=%d align=%d fields=%d",
			actual, actual.Size(), actual.Align(), actual.NumField(),
			baseline, baseline.Size(), baseline.Align(), baseline.NumField())
	}
	for i := 0; i < actual.NumField(); i++ {
		left, right := actual.Field(i), baseline.Field(i)
		if left.Name != right.Name || left.Offset != right.Offset ||
			left.Type.Size() != right.Type.Size() || left.Type.Align() != right.Type.Align() ||
			!recursive[left.Name] && left.Type != right.Type {
			t.Errorf("field %d differs: %+v / %+v", i, left, right)
		}
	}
}

func TestRepresentationsPreserveFields(t *testing.T) {
	for _, fixture := range benchmarkFixtures(t) {
		t.Run(fixture.name, func(t *testing.T) {
			checkEquivalent(t, fixture.nodes)
			mix := make(map[token.Kind]int)
			var walk func([]seed)
			walk = func(nodes []seed) {
				for _, node := range nodes {
					mix[node.kind]++
					walk(node.children)
				}
			}
			walk(fixture.nodes)
			t.Logf("roots=%d total nodes=%d token mix=%v", len(fixture.nodes), fixture.count, mix)
		})
	}
	t.Run("Empty", func(t *testing.T) { checkEquivalent(t, nil) })
	t.Run("LexicalVariants", func(t *testing.T) {
		checkEquivalent(t, sourceSeeds(t, "r#match func 42 3.5 2i 'x' \"hello\" + ; () [] {}\nname\n"))
	})
	t.Run("FragmentAndIssuedSpans", func(t *testing.T) {
		scope := token.NewScope("fragment")
		context := scope.NewContext()
		view, err := token.ParseText("(x + 42)", context)
		if err != nil {
			t.Fatal(err)
		}
		span, err := scope.NewSpan(0, 8)
		if err != nil {
			t.Fatal(err)
		}
		group := view.Tokens()[0].WithSpan(span).WithDelimiterSpans(span, span)
		fragment, err := scope.CaptureFragment(token.ExprFragment, token.New(group), context)
		if err != nil {
			t.Fatal(err)
		}
		source := seedsFromStream(scope, token.New(fragment.WithSpan(span)), make(map[uint32]*inputReference))
		checkEquivalent(t, source)
		candidate := buildCandidate(source)[0]
		payload := candidate.Payload.(*Fragment)
		if candidate.Input == nil || candidate.Input.id != fragment.Reference() ||
			payload.Kind != token.ExprFragment || payload.View[0].Input != nil {
			t.Fatal("fragment category, original identity or untrusted child view changed")
		}
	})
}

func checkEquivalent(t *testing.T, source []seed) {
	t.Helper()
	flat, candidate := buildFlat(source), buildCandidate(source)
	if !reflect.DeepEqual(flatSeeds(flat), source) || !reflect.DeepEqual(candidateSeeds(candidate), source) {
		t.Fatal("representation lost or changed token fields")
	}
	if left, right := traverseFlat(flat), TraverseCandidate(candidate); left != right {
		t.Fatalf("traversal scores differ: %d / %d", left, right)
	}
}

func TestBuildsOwnArraysAndPayloads(t *testing.T) {
	source := sourceSeeds(t, "(x)")
	flatA, flatB := buildFlat(source), buildFlat(source)
	candidateA, candidateB := buildCandidate(source), buildCandidate(source)
	a, b := candidateA[0].Payload.(*Group), candidateB[0].Payload.(*Group)
	a.Children[0].Payload.(*Ident).Text = "changed"
	flatA[0].children.tokens[0].text = "changed"
	if b.Children[0].Payload.(*Ident).Text != "x" || flatB[0].children.tokens[0].text != "x" ||
		source[0].children[0].text != "x" {
		t.Fatal("builds share mutable arrays or payload objects")
	}
	span, err := token.LocalSpan(100, 200)
	if err != nil {
		t.Fatal(err)
	}
	candidateA[0].Span, flatA[0].span = span, span
	if candidateB[0].Span == span || flatB[0].span == span {
		t.Fatal("builds share token headers")
	}
}
