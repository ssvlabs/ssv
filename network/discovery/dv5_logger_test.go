package discovery

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
)

func Test_dv5Logger_Log(t *testing.T) {
	type fields struct {
		logger *zap.Logger
	}
	type args struct {
		r *log.Record
	}
	logger := logging.TestLogger(t)
	ctx := make([]interface{}, 1)
	ctx[0] = "test context"
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name:   "lvl error",
			fields: fields{logger: logger},
			args: args{
				r: &log.Record{
					Time: time.Now(),
					Lvl:  1,
					Msg:  "test",
					Ctx:  ctx,
				},
			},
			wantErr: false,
		},
		{
			name:   "lvl warn",
			fields: fields{logger: logger},
			args: args{
				r: &log.Record{
					Time: time.Now(),
					Lvl:  2,
					Msg:  "test",
					Ctx:  ctx,
				},
			},
			wantErr: false,
		},
		{
			name:   "lvl info",
			fields: fields{logger: logger},
			args: args{
				r: &log.Record{
					Time: time.Now(),
					Lvl:  3,
					Msg:  "test",
					Ctx:  ctx,
				},
			},
			wantErr: false,
		},
		{
			name:   "lvl debug",
			fields: fields{logger: logger},
			args: args{
				r: &log.Record{
					Time: time.Now(),
					Lvl:  4,
					Msg:  "test",
					Ctx:  ctx,
				},
			},
			wantErr: false,
		},
		{
			name:   "lvl trace",
			fields: fields{logger: logger},
			args: args{
				r: &log.Record{
					Time: time.Now(),
					Lvl:  5,
					Msg:  "test",
					Ctx:  ctx,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dvl := &dv5Logger{
				logger: tt.fields.logger,
			}
			if err := dvl.Log(tt.args.r); (err != nil) != tt.wantErr {
				t.Errorf("dv5Logger.Log() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
