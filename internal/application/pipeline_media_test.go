package application

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPipelineValidate(t *testing.T) {
	tests := []struct {
		name        string
		req         PipelineRequest
		expectError bool
	}{
		{
			name: "valid request",
			req: PipelineRequest{
				Inputs:  []InputFile{{FileURL: "http://example.com/video.mp4"}},
				Outputs: []OutputSpec{{Options: []FFmpegOption{{Option: "-c:v", Argument: json.RawMessage(`"libx264"`)}}}},
			},
			expectError: false,
		},
		{
			name: "missing inputs",
			req: PipelineRequest{
				Inputs:  nil,
				Outputs: []OutputSpec{{Options: []FFmpegOption{{Option: "-c:v", Argument: json.RawMessage(`"libx264"`)}}}},
			},
			expectError: true,
		},
		{
			name: "missing outputs",
			req: PipelineRequest{
				Inputs:  []InputFile{{FileURL: "http://example.com/video.mp4"}},
				Outputs: nil,
			},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validate(tc.req)
			if tc.expectError && err == nil {
				t.Fatalf("expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Fatalf("did not expect error but got: %v", err)
			}
		})
	}
}

func TestBuildFFmpegArgs(t *testing.T) {
	tests := []struct {
		name       string
		inputs     []InputFile
		localPaths []string
		filters    []FilterBlock
		outputs    []OutputSpec
		outPath    string
		expected   []string
		expectErr  bool
	}{
		{
			name: "basic",
			inputs: []InputFile{
				{Options: []FFmpegOption{{Option: "-ss", Argument: json.RawMessage(`"10"`)}}},
			},
			localPaths: []string{"/tmp/input.mp4"},
			filters:    []FilterBlock{{Filter: "scale=1920:1080"}},
			outputs: []OutputSpec{
				{Options: []FFmpegOption{{Option: "-c:v", Argument: json.RawMessage(`"libx264"`)}}},
			},
			outPath: "/tmp/output.mp4",
			expected: []string{
				"-ss", "10",
				"-i", "/tmp/input.mp4",
				"-filter_complex", "scale=1920:1080",
				"-c:v", "libx264",
				"/tmp/output.mp4",
			},
			expectErr: false,
		},
		{
			name: "multiple inputs and filters",
			inputs: []InputFile{
				{Options: []FFmpegOption{{Option: "-t", Argument: json.RawMessage(`"5"`)}}},
				{Options: []FFmpegOption{}},
			},
			localPaths: []string{"/tmp/1.mp4", "/tmp/2.mp4"},
			filters:    []FilterBlock{{Filter: "[0:v][1:v]vstack[v]"}, {Filter: "[v]scale=100:100"}},
			outputs: []OutputSpec{
				{Options: []FFmpegOption{{Option: "-c:v", Argument: json.RawMessage(`"copy"`)}}},
			},
			outPath: "/tmp/out.mp4",
			expected: []string{
				"-t", "5",
				"-i", "/tmp/1.mp4",
				"-i", "/tmp/2.mp4",
				"-filter_complex", "[0:v][1:v]vstack[v];[v]scale=100:100",
				"-c:v", "copy",
				"/tmp/out.mp4",
			},
			expectErr: false,
		},
		{
			name: "number argument",
			inputs: []InputFile{
				{Options: []FFmpegOption{{Option: "-ss", Argument: json.RawMessage(`10`)}}},
			},
			localPaths: []string{"/tmp/1.mp4"},
			filters:    nil,
			outputs:    nil,
			outPath:    "/tmp/out.mp4",
			expected: []string{
				"-ss", "10",
				"-i", "/tmp/1.mp4",
				"/tmp/out.mp4",
			},
			expectErr: false,
		},
		{
			name: "invalid argument",
			inputs: []InputFile{
				{Options: []FFmpegOption{{Option: "-ss", Argument: json.RawMessage(`{"bad": "format"}`)}}},
			},
			localPaths: []string{"/tmp/1.mp4"},
			expectErr:  true,
		},
		{
			name: "invalid output argument",
			inputs: []InputFile{
				{Options: []FFmpegOption{}},
			},
			localPaths: []string{"/tmp/1.mp4"},
			outputs: []OutputSpec{
				{Options: []FFmpegOption{{Option: "-c:v", Argument: json.RawMessage(`{"bad": "format"}`)}}},
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args, err := buildFFmpegArgs(tc.inputs, tc.localPaths, tc.filters, tc.outputs, tc.outPath)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(args, tc.expected) {
				t.Fatalf("expected args %v, got %v", tc.expected, args)
			}
		})
	}
}
