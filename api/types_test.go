package api

import (
	"reflect"
	"testing"
)

func TestHex_MarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		h       Hex
		want    []byte
		wantErr bool
	}{
		{
			name:    "test1",
			h:       []byte{0, 1, 2, 3, 4, 5, 6, 7, 8},
			want:    []byte{34, 48, 48, 48, 49, 48, 50, 48, 51, 48, 52, 48, 53, 48, 54, 48, 55, 48, 56, 34},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.h.MarshalJSON()
			if (err != nil) != tt.wantErr {
				t.Errorf("Hex.MarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Hex.MarshalJSON() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHex_UnmarshalJSON(t *testing.T) {
	type args struct {
		data []byte
	}
	tests := []struct {
		name    string
		h       *Hex
		args    args
		wantErr bool
	}{
		{
			name:    "ok",
			h:       &Hex{},
			args:    args{data: []byte{34, 48, 48, 48, 49, 48, 50, 48, 51, 48, 52, 48, 53, 48, 54, 48, 55, 48, 56, 34}},
			wantErr: false,
		},
		{
			name:    "wrong data",
			h:       &Hex{},
			args:    args{data: []byte{}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.h.UnmarshalJSON(tt.args.data); (err != nil) != tt.wantErr {
				t.Errorf("Hex.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHex_Bind(t *testing.T) {
	type args struct {
		value string
	}
	tests := []struct {
		name    string
		h       *Hex
		args    args
		wantErr bool
	}{
		{
			name:    "ok",
			h:       &Hex{},
			args:    args{value: "01010101"},
			wantErr: false,
		},
		{
			name:    "error hex",
			h:       &Hex{},
			args:    args{value: "wrong hex"},
			wantErr: true,
		},
		{
			name:    "hex len 0",
			h:       &Hex{},
			args:    args{value: ""},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.h.Bind(tt.args.value); (err != nil) != tt.wantErr {
				t.Errorf("Hex.Bind() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHexSlice_Bind(t *testing.T) {
	type args struct {
		value string
	}
	tests := []struct {
		name    string
		hs      *HexSlice
		args    args
		wantErr bool
	}{
		{
			name:    "ok",
			hs:      &HexSlice{},
			args:    args{value: "01010101010101,0102"},
			wantErr: false,
		},
		{
			name:    "hex len 0",
			hs:      &HexSlice{},
			args:    args{value: ""},
			wantErr: false,
		},
		{
			name:    "error",
			hs:      &HexSlice{},
			args:    args{value: "01010101010101,0x0101"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.hs.Bind(tt.args.value); (err != nil) != tt.wantErr {
				t.Errorf("HexSlice.Bind() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUint64Slice_Bind(t *testing.T) {
	type args struct {
		value string
	}
	tests := []struct {
		name    string
		us      *Uint64Slice
		args    args
		wantErr bool
	}{
		{
			name:    "ok",
			us:      &Uint64Slice{},
			args:    args{value: "1,10,20"},
			wantErr: false,
		},
		{
			name:    "nil",
			us:      &Uint64Slice{},
			args:    args{value: ""},
			wantErr: false,
		},
		{
			name:    "error",
			us:      &Uint64Slice{},
			args:    args{value: "1,10,hello"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.us.Bind(tt.args.value); (err != nil) != tt.wantErr {
				t.Errorf("Uint64Slice.Bind() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
