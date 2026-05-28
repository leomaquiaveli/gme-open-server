package application

import (
	"testing"
)

func TestValidateClips(t *testing.T) {
	tests := []struct {
		name        string
		req         ClipsRequest
		expectError bool
	}{
		{
			name: "valid request simple",
			req: ClipsRequest{
				InputURL: "http://example.com/video.mp4",
				Clips: []ClipSpec{
					{Start: 0, Duration: 10},
				},
			},
			expectError: false,
		},
		{
			name: "missing input url",
			req: ClipsRequest{
				InputURL: "",
				Clips: []ClipSpec{
					{Start: 0, Duration: 10},
				},
			},
			expectError: true,
		},
		{
			name: "missing clips",
			req: ClipsRequest{
				InputURL: "http://example.com/video.mp4",
				Clips:    nil,
			},
			expectError: true,
		},
		{
			name: "invalid global mode",
			req: ClipsRequest{
				InputURL: "http://example.com/video.mp4",
				Mode:     "invalid_mode",
				Clips: []ClipSpec{
					{Start: 0, Duration: 10},
				},
			},
			expectError: true,
		},
		{
			name: "invalid global crf low",
			req: ClipsRequest{
				InputURL: "http://example.com/video.mp4",
				CRF:      0, // 0 is allowed (default)
				Clips: []ClipSpec{
					{Start: 0, Duration: 10},
				},
			},
			expectError: false,
		},
		{
			name: "invalid global crf high",
			req: ClipsRequest{
				InputURL: "http://example.com/video.mp4",
				CRF:      52,
				Clips: []ClipSpec{
					{Start: 0, Duration: 10},
				},
			},
			expectError: true,
		},
		{
			name: "invalid clip mode",
			req: ClipsRequest{
				InputURL: "http://example.com/video.mp4",
				Clips: []ClipSpec{
					{Start: 0, Duration: 10, Mode: "wrong"},
				},
			},
			expectError: true,
		},
		{
			name: "clip end <= start",
			req: ClipsRequest{
				InputURL: "http://example.com/video.mp4",
				Clips: []ClipSpec{
					{Start: 10, End: 5},
				},
			},
			expectError: true,
		},
		{
			name: "clip duration and end",
			req: ClipsRequest{
				InputURL: "http://example.com/video.mp4",
				Clips: []ClipSpec{
					{Start: 0, End: 10, Duration: 10},
				},
			},
			expectError: true,
		},
		{
			name: "clip invalid scroll_x",
			req: ClipsRequest{
				InputURL: "http://example.com/video.mp4",
				Clips: []ClipSpec{
					{Start: 0, Duration: 10, ScrollX: 150},
				},
			},
			expectError: true,
		},
		{
			name: "clip valid scroll_x",
			req: ClipsRequest{
				InputURL: "http://example.com/video.mp4",
				Clips: []ClipSpec{
					{Start: 0, Duration: 10, ScrollX: 50},
				},
			},
			expectError: false,
		},
		{
			name: "invalid global preset",
			req: ClipsRequest{
				InputURL: "http://example.com/video.mp4",
				Preset:   "magic",
				Clips: []ClipSpec{
					{Start: 0, Duration: 10},
				},
			},
			expectError: true,
		},
		{
			name: "invalid clip preset",
			req: ClipsRequest{
				InputURL: "http://example.com/video.mp4",
				Clips: []ClipSpec{
					{Start: 0, Duration: 10, Preset: "invalid_preset"},
				},
			},
			expectError: true,
		},
		{
			name: "invalid clip crf",
			req: ClipsRequest{
				InputURL: "http://example.com/video.mp4",
				Clips: []ClipSpec{
					{Start: 0, Duration: 10, CRF: 60},
				},
			},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateClips(tc.req)
			if tc.expectError && err == nil {
				t.Fatalf("expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Fatalf("did not expect error but got: %v", err)
			}
		})
	}
}
