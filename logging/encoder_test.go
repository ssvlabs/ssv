package logging

import (
	"testing"

	"github.com/bloxapp/ssv/logging/mocks"
	"github.com/golang/mock/gomock"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

//go:generate mockgen  -package=mocks -destination=mocks/zapcore.go go.uber.org/zap/zapcore Encoder

func Test_debugServicesEncoder_EncodeEntry(t *testing.T) {
	type fields struct {
		Encoder        func(t *testing.T, ctrl *gomock.Controller) zapcore.Encoder
		excludeLoggers []string
	}
	type args struct {
		entry  zapcore.Entry
		fields []zapcore.Field
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *buffer.Buffer
		wantErr bool
	}{
		{
			name: "exclude",
			fields: fields{
				Encoder: func(t *testing.T, ctrl *gomock.Controller) zapcore.Encoder {
					encoder := mocks.NewMockEncoder(ctrl)
					return encoder
				},
				excludeLoggers: []string{"DiscoveryV5Logger"},
			},
			args: args{
				entry: zapcore.Entry{
					Level:      zapcore.DebugLevel,
					LoggerName: "SSV-Node:v0.4.0.P2PNetwork.DiscoveryV5Logger",
				},
				fields: []zapcore.Field{},
			},
		},
		{
			name: "include",
			fields: fields{
				Encoder: func(t *testing.T, ctrl *gomock.Controller) zapcore.Encoder {
					encoder := mocks.NewMockEncoder(ctrl)
					encoder.EXPECT().EncodeEntry(gomock.Any(), gomock.Any()).Return(nil, nil)
					return encoder
				},
				excludeLoggers: []string{"exclude"},
			},
			args: args{
				entry: zapcore.Entry{
					Level:      zapcore.DebugLevel,
					LoggerName: "SSV-Node:v0.4.0.P2PNetwork.DiscoveryV5Logger",
				},
				fields: []zapcore.Field{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			d := debugServicesEncoder{
				Encoder:        tt.fields.Encoder(t, ctrl),
				excludeLoggers: tt.fields.excludeLoggers,
			}
			_, err := d.EncodeEntry(tt.args.entry, tt.args.fields)
			if (err != nil) != tt.wantErr {
				t.Errorf("debugServicesEncoder.EncodeEntry() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
