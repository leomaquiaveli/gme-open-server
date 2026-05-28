package application

import (
	"testing"
)

func ptr(f float64) *float64 { return &f }

func TestBuildScrollXExpr_Static(t *testing.T) {
	cases := []struct {
		name    string
		scrollX float64
		margin  string
		want    string
	}{
		{"zero", 0, "M", "M*(1+(0/100))"},
		{"positive", 25, "M", "M*(1+(25/100))"},
		{"negative", -50, "M", "M*(1+(-50/100))"},
	}
	for _, tc := range cases {
		got := buildScrollXExpr(tc.scrollX, nil, tc.margin)
		if got != tc.want {
			t.Errorf("[%s] got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestBuildScrollXExpr_SingleKeyframe(t *testing.T) {
	kfs := []Keyframe{{Time: 2.0, ScrollX: ptr(50)}}
	margin := "M"

	// Before first keyframe (time < 2): use static scrollX=0.
	// After first keyframe (time >= 2): use scrollX=50.
	got := buildScrollXExpr(0, kfs, margin)

	// Expected: if(lt(t\,2)\,M*(1+(0/100))\,M*(1+(50/100)))
	want := `if(lt(t\,2)\,M*(1+(0/100))\,M*(1+(50/100)))`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestBuildScrollXExpr_MultipleKeyframes(t *testing.T) {
	kfs := []Keyframe{
		{Time: 1.0, ScrollX: ptr(10)},
		{Time: 3.0, ScrollX: ptr(40)},
	}
	margin := "M"

	// Segments:
	// t < 1: static scrollX=0
	// 1 <= t < 3: scrollX=10
	// t >= 3: scrollX=40
	got := buildScrollXExpr(0, kfs, margin)
	want := `if(lt(t\,1)\,M*(1+(0/100))\,if(lt(t\,3)\,M*(1+(10/100))\,M*(1+(40/100))))`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestBuildScrollXExpr_KeyframesIgnoredWhenNoScrollX(t *testing.T) {
	// Keyframes with only Zoom set — no ScrollX — must behave like static.
	kfs := []Keyframe{{Time: 1.0, Zoom: ptr(50)}}
	got := buildScrollXExpr(20, kfs, "M")
	want := "M*(1+(20/100))"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildScrollXExpr_KeyframeAtTimeZero(t *testing.T) {
	// First keyframe at t=0: no guard needed before it.
	kfs := []Keyframe{{Time: 0.0, ScrollX: ptr(30)}}
	got := buildScrollXExpr(0, kfs, "M")
	// No "if(lt(t\,0)" prefix because kfs[0].Time == 0.
	want := "M*(1+(30/100))"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ─── buildZoomExpr ───────────────────────────────────────────────────────────

func TestBuildZoomExpr_Static(t *testing.T) {
	cases := []struct {
		name   string
		zoom   float64
		wantFactor string
	}{
		{"zero zoom → factor 1", 0, "1"},
		{"100% zoom → factor 2", 100, "2"},
		{"50% zoom → factor 1.5", 50, "1.5"},
	}
	for _, tc := range cases {
		got := buildZoomExpr(tc.zoom, nil)
		if got != tc.wantFactor {
			t.Errorf("[%s] got %q, want %q", tc.name, got, tc.wantFactor)
		}
	}
}

func TestBuildZoomExpr_SingleKeyframe(t *testing.T) {
	// zoom=0 static, keyframe at t=2 zooms to 100% (factor=2).
	kfs := []Keyframe{{Time: 2.0, Zoom: ptr(100)}}
	got := buildZoomExpr(0, kfs)
	// Before t=2: factor=1 (static). At t>=2: factor=2.
	want := "if(lt(it,2),1,2)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildZoomExpr_MultipleKeyframes(t *testing.T) {
	kfs := []Keyframe{
		{Time: 1.0, Zoom: ptr(0)},   // factor 1
		{Time: 3.0, Zoom: ptr(100)}, // factor 2
	}
	got := buildZoomExpr(0, kfs)
	// t < 1: static (factor 1)
	// 1 <= t < 3: factor 1
	// t >= 3: factor 2
	want := "if(lt(it,1),1,if(lt(it,3),1,2))"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildZoomExpr_KeyframesIgnoredWhenNoZoom(t *testing.T) {
	// Keyframes with only ScrollX set — no Zoom — must behave like static.
	kfs := []Keyframe{{Time: 1.0, ScrollX: ptr(50)}}
	got := buildZoomExpr(50, kfs)
	want := "1.5"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildZoomExpr_KeyframeAtTimeZero(t *testing.T) {
	// First keyframe at t=0: no guard prefix.
	kfs := []Keyframe{{Time: 0.0, Zoom: ptr(100)}}
	got := buildZoomExpr(0, kfs)
	want := "2"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildZoomExpr_KeyframesOutOfOrder(t *testing.T) {
	// Keyframes provided out of order — must be sorted by time.
	kfs := []Keyframe{
		{Time: 3.0, Zoom: ptr(100)},
		{Time: 1.0, Zoom: ptr(0)},
	}
	got := buildZoomExpr(0, kfs)
	want := "if(lt(it,1),1,if(lt(it,3),1,2))"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
