package main

import (
	"strings"
	"testing"
)

func TestSplitNoSyncArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantNoSync bool
		wantArgs   []string
		wantErr    bool
	}{
		{
			name:       "no-sync before separator",
			args:       []string{"--no-sync", "--", "--reuse-window"},
			wantNoSync: true,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "no-sync bool false",
			args:       []string{"--no-sync=false", "--reuse-window"},
			wantNoSync: false,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:    "no-sync invalid value",
			args:    []string{"--no-sync=maybe"},
			wantErr: true,
		},
		{
			name:       "pass-through without separator",
			args:       []string{"--reuse-window"},
			wantNoSync: false,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "no-sync after separator",
			args:       []string{"--", "--no-sync"},
			wantNoSync: false,
			wantArgs:   []string{"--no-sync"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNoSync, gotArgs, err := splitNoSyncArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotNoSync != tt.wantNoSync {
				t.Fatalf("expected noSync=%v, got %v", tt.wantNoSync, gotNoSync)
			}
			if strings.Join(gotArgs, ",") != strings.Join(tt.wantArgs, ",") {
				t.Fatalf("expected args %v, got %v", tt.wantArgs, gotArgs)
			}
		})
	}
}
