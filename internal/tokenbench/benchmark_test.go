package tokenbench

import (
	"testing"

	"github.com/goxide-lang/syntax/token"
)

// Package-level sinks force the completed trees and copies to escape.
var (
	flatSink      []flatToken
	candidateSink []Candidate
	scoreSink     uint64
)

func resetTreeSinks(b *testing.B) {
	clear := func() {
		flatSink = nil
		candidateSink = nil
	}
	// Do not retain the preceding representation's tree and alter GC pacing.
	clear()
	b.Cleanup(clear)
}

// BenchmarkBuild includes all tree arrays and all candidate payload allocations.
// Neither side includes lexing, validation, strings or shared input references.
func BenchmarkBuild(b *testing.B) {
	for _, fixture := range benchmarkFixtures(b) {
		b.Run(fixture.name, func(b *testing.B) {
			b.Run("Flat", func(b *testing.B) {
				resetTreeSinks(b)
				b.ReportAllocs()
				for b.Loop() {
					flatSink = buildFlat(fixture.nodes)
				}
				b.ReportMetric(float64(fixture.count), "nodes/op")
			})
			b.Run("EnumPayload", func(b *testing.B) {
				resetTreeSinks(b)
				b.ReportAllocs()
				for b.Loop() {
					candidateSink = buildCandidate(fixture.nodes)
				}
				b.ReportMetric(float64(fixture.count), "nodes/op")
			})
		})
	}
}

// BenchmarkTraverse walks prebuilt trees and consumes every meaningful field.
func BenchmarkTraverse(b *testing.B) {
	for _, fixture := range benchmarkFixtures(b) {
		b.Run(fixture.name, func(b *testing.B) {
			flat, candidate := buildFlat(fixture.nodes), buildCandidate(fixture.nodes)
			b.Run("Flat", func(b *testing.B) {
				resetTreeSinks(b)
				b.ReportAllocs()
				for b.Loop() {
					scoreSink = traverseFlat(flat)
				}
				b.ReportMetric(float64(fixture.count), "nodes/op")
			})
			b.Run("EnumPayload", func(b *testing.B) {
				resetTreeSinks(b)
				b.ReportAllocs()
				for b.Loop() {
					scoreSink = TraverseCandidate(candidate)
				}
				b.ReportMetric(float64(fixture.count), "nodes/op")
			})
		})
	}
}

// BenchmarkCopyShallow models Tokens/WithSpan-style copies of the root stream.
// Child streams and immutable payloads are intentionally shared on both sides.
func BenchmarkCopyShallow(b *testing.B) {
	span, err := token.LocalSpan(100, 200)
	if err != nil {
		b.Fatal(err)
	}
	for _, fixture := range benchmarkFixtures(b) {
		b.Run(fixture.name, func(b *testing.B) {
			flat, candidate := buildFlat(fixture.nodes), buildCandidate(fixture.nodes)
			b.Run("Flat", func(b *testing.B) {
				resetTreeSinks(b)
				b.ReportAllocs()
				for b.Loop() {
					copied := append([]flatToken(nil), flat...)
					for i := range copied {
						copied[i].span = span
					}
					flatSink = copied
				}
				b.ReportMetric(float64(len(flat)), "roots/op")
			})
			b.Run("EnumPayload", func(b *testing.B) {
				resetTreeSinks(b)
				b.ReportAllocs()
				for b.Loop() {
					copied := append([]Candidate(nil), candidate...)
					for i := range copied {
						copied[i].Span = span
					}
					candidateSink = copied
				}
				b.ReportMetric(float64(len(candidate)), "roots/op")
			})
		})
	}
}
