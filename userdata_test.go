package luart_test

import (
	"testing"

	"github.com/matjam/luart"
)

type point struct{ x, y int }

func TestUserDataGeneric(t *testing.T) {
	tests := []struct {
		name string
		push func(l *luart.State)
		want bool
	}{
		{"holds T", func(l *luart.State) { l.PushUserData(&point{1, 2}) }, true},
		{"holds other type", func(l *luart.State) { l.PushUserData("nope") }, false},
		{"not userdata", func(l *luart.State) { l.PushNumber(1) }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := luart.NewState()
			tt.push(l)
			p, ok := l.UserData[*point](-1)
			if ok != tt.want {
				t.Fatalf("ok = %v, want %v", ok, tt.want)
			}
			if ok && *p != (point{1, 2}) {
				t.Fatalf("got %v", *p)
			}
		})
	}
}

func TestCheckUserDataGeneric(t *testing.T) {
	tests := []struct {
		name    string
		push    func(l *luart.State)
		wantErr bool
	}{
		{"named userdata holding T", func(l *luart.State) {
			l.PushUserData(&point{3, 4})
			l.SetMetaTableNamed("point")
		}, false},
		{"named userdata holding other type", func(l *luart.State) {
			l.PushUserData("nope")
			l.SetMetaTableNamed("point")
		}, true},
		{"unnamed userdata", func(l *luart.State) { l.PushUserData(&point{3, 4}) }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := luart.NewState()
			l.NewMetaTable("point")
			l.Pop(1)
			var got *point
			l.PushGoFunction(func(l *luart.State) int {
				got = l.CheckUserData[*point](1, "point")
				return 0
			})
			tt.push(l)
			err := l.ProtectedCall(1, 0, 0)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && *got != (point{3, 4}) {
				t.Fatalf("got %v", *got)
			}
		})
	}
}
