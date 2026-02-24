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
		wantQuiet  bool
		wantArgs   []string
		wantErr    bool
	}{
		{
			name:       "no-sync before separator",
			args:       []string{"--no-sync", "--", "--reuse-window"},
			wantNoSync: true,
			wantQuiet:  false,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "quiet before separator",
			args:       []string{"--quiet", "--no-sync", "--", "--reuse-window"},
			wantNoSync: true,
			wantQuiet:  true,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "quiet bool true",
			args:       []string{"--quiet=true", "--reuse-window"},
			wantNoSync: false,
			wantQuiet:  true,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "no-sync bool false",
			args:       []string{"--no-sync=false", "--reuse-window"},
			wantNoSync: false,
			wantQuiet:  false,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "quiet bool false consumed",
			args:       []string{"--quiet=false", "--reuse-window"},
			wantNoSync: false,
			wantQuiet:  false,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:    "no-sync invalid value",
			args:    []string{"--no-sync=maybe"},
			wantErr: true,
		},
		{
			name:    "quiet invalid value",
			args:    []string{"--quiet=maybe"},
			wantErr: true,
		},
		{
			name:       "pass-through without separator",
			args:       []string{"--reuse-window"},
			wantNoSync: false,
			wantQuiet:  false,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "no-sync after separator",
			args:       []string{"--", "--no-sync"},
			wantNoSync: false,
			wantQuiet:  false,
			wantArgs:   []string{"--no-sync"},
		},
		{
			name:       "quiet after separator",
			args:       []string{"--", "--quiet"},
			wantNoSync: false,
			wantQuiet:  false,
			wantArgs:   []string{"--quiet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNoSync, gotQuiet, gotArgs, err := splitNoSyncArgs(tt.args)
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
			if gotQuiet != tt.wantQuiet {
				t.Fatalf("expected quiet=%v, got %v", tt.wantQuiet, gotQuiet)
			}
			if strings.Join(gotArgs, ",") != strings.Join(tt.wantArgs, ",") {
				t.Fatalf("expected args %v, got %v", tt.wantArgs, gotArgs)
			}
		})
	}
}
