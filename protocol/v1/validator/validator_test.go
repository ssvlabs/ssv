package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	qbftStorage "github.com/bloxapp/ssv/ibft/storage"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbft2 "github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	forksfactory "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks/factory"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/factory"
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/ssv"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("test", zapcore.DebugLevel, nil)
}

func TestIdentifierTest(t *testing.T) {
	node := testingValidator(t, true, 4, []byte{1, 2, 3, 4})
	require.True(t, oneOfIBFTIdentifiers(node, []byte{1, 2, 3, 4}))
	require.False(t, oneOfIBFTIdentifiers(node, []byte{1, 2, 3, 3}))
}

func oneOfIBFTIdentifiers(v *Validator, toMatch []byte) bool {
	for _, i := range v.ibfts {
		if bytes.Equal(i.GetIdentifier(), toMatch) {
			return true
		}
	}
	return false
}

func TestConvertDutyRunner(t *testing.T) {
	require.NoError(t, bls.Init(bls.BLS12_381))

	msgsInput := []byte("[\n         {\n            \"MsgType\":0,\n            \"MsgID\":[\n               142,\n               128,\n               6,\n               101,\n               81,\n               168,\n               27,\n               49,\n               130,\n               88,\n               112,\n               158,\n               218,\n               247,\n               221,\n               31,\n               99,\n               205,\n               104,\n               106,\n               14,\n               77,\n               184,\n               178,\n               155,\n               187,\n               122,\n               207,\n               230,\n               86,\n               8,\n               103,\n               122,\n               245,\n               165,\n               39,\n               217,\n               68,\n               142,\n               228,\n               120,\n               53,\n               72,\n               94,\n               2,\n               181,\n               11,\n               192,\n               0,\n               0,\n               0,\n               0\n            ],\n            \"Data\":\"eyJTaWduYXR1cmUiOiJ0Mlkrb2lBQ0g1U3dGdy9Vczg5Q3lGc091akxLZnVPV2FhVUF6UEJ5WGtIOGZORDJVVWpkZWpKSXppVUovVTRQQnlOTkpuUVF1aGJmU0l1R2RmNWcvb3cyM2x3TTlEVTZrMERXaElBeFBYSURMOENQN0haSzZZYzR6Zk5wOStQbSIsIlNpZ25lcnMiOlsxXSwiTWVzc2FnZSI6eyJNc2dUeXBlIjowLCJIZWlnaHQiOjAsIlJvdW5kIjoxLCJJZGVudGlmaWVyIjoiam9BR1pWR29HekdDV0hDZTJ2ZmRIMlBOYUdvT1RiaXltN3Q2eitaV0NHZDY5YVVuMlVTTzVIZzFTRjRDdFF2QUFBQUFBQT09IiwiRGF0YSI6ImV5SkVZWFJoSWpvaVpYbEtSV1JZVWpWSmFuQTNTV3hTTldOSFZXbFBha0Z6U1d4Q01WbHJkR3hsVTBrMlYzcEZNRTFwZDNoTmFtZHpUbWwzZUUxRVJYTlBSRVZ6VFZSWk5FeEVTVE5NUkZFMVRFUkZlazFEZHpSUFEzZDRUVlJKYzAxVVZUUk1SRWw0VDBOM2VVNUVZM05OYWtsNFRFUk5lRXhFYXpWTVJFbDNUbE4zZUUxRVVYTk5WRUV5VEVSRk1FeEVZek5NUkVVMFRrTjNlRTU2WjNOTlZGVXhURVJGTkU1NWQzaE5ha2x6VFdwQk0weEVTWHBOUTNjMFRtbDNORXhFUlhkTmVYZDRUV3BKYzAxcVVURk1SRVV5VGxOM2VrOVRkM2xOVkdOelRtcG5jMDFVVVhsTVJFbDVUME4zZUUxcVFYTk9WRTF6VG5wSmMwOVVVWE5OYVhkNFQwUkZjMDFVUlhOTlZHdDVXRk4zYVZVeWVIWmtRMGsyVFZSSmMwbHNXbWhpUjJ4cldWaFNkbU5yYkhWYVIxWTBTV3B2ZUV4RFNrUmlNakYwWVZoU01GcFhWa3BpYlZKc1pVTkpOazE1ZDJsUk1qbDBZbGRzTUdSSFZteFVSMVoxV2pOU2IwbHFiM2hOYW1kelNXdE9kbUpYTVhCa1NGSnNXbGhPUW1SR1RuTmlNMUZwVDJwTk1reERTbGRaVjNod1drZEdNR0l6U2tSaU1qRjBZVmhTTUZwWFZrcGliVkpzWlVOSk5rMVVSamxNUTBwQ1pFaFNiR016VW1oa1IyeDJZbXRTYUdSSFJXbFBibk5wWXpKNGRtUkRTVFpKYWtWNVNXbDNhV0ZYTld0YVdHZHBUMmxKZWtscGQybFpiVlpvV1RJNWRWZ3lTbk5pTWs1eVdETktkbUl6VVdsUGFVbDNaVVJCZUUxRVNYZE5la0V3VFVSVmQwNXFRVE5OUkdkM1QxUkNhRTFFUlhkTmFrRjZUVVJSZDA1VVFUSk5SR04zVDBSQk5VMUhSWGROVkVGNVRVUk5kMDVFUVRGTlJGbDNUbnBCTkUxRWEzZFpWRUY0VFVSSmFVeERTbnBpTTFaNVdUSlZhVTl1YzJsYVdFSjJXVEpuYVU5cFNYZEphWGRwWTIwNWRtUkRTVFpKYWtJMFRVUkZkMDFxUVhwTlJGRjNUbFJCTWsxRVkzZFBSRUUxVFVkRmQwMVVRWGxOUkUxM1RrUkJNVTFFV1hkT2VrRTBUVVJyZDFsVVFYaE5SRWwzVFhwQk1FMUVWWGRPYWtFelRVUm5kMDlVUW1oTlJFVjNUV2xLT1V4RFNqQlpXRXB1V2xoUmFVOXVjMmxhV0VKMldUSm5hVTlwU1hoSmFYZHBZMjA1ZG1SRFNUWkpha0kwVFVSRmQwMXFRWHBOUkZGM1RsUkJNazFFWTNkUFJFRTFUVWRGZDAxVVFYbE5SRTEzVGtSQk1VMUVXWGRPZWtFMFRVUnJkMWxVUVhoTlJFbDNUWHBCTUUxRVZYZE9ha0V6VFVSbmQwOVVRbWhOUkVWM1RXbEtPV1pUZDJsUmJYaDJXVEowUlZsWVVtaEphbkIxWkZkNGMweERTa0phTW1SNVdsZGthR1JIVmtKaWJWSlJZMjA1ZGxwcFNUWmlibFp6WWtOM2FWVXpiSFZaTUU1MllsY3hjR1JJVW14YVZVcHpZakpPY2xWdE9YWmtRMGsyVjNwQmMwMURkM2RNUkVGelRVTjNkMHhFUVhOTlEzZDNURVJCYzAxRGQzZE1SRUZ6VFVOM2QweEVRWE5OUTNkM1RFUkJjMDFEZDNkTVJFRnpUVU4zZDB4RVFYTk5RM2QzVEVSQmMwMURkM2RNUkVGelRVWXdjMGxzVGpWaWJVNUVZakl4ZEdGWVVqQmFWMVpFWWpJMU1HTnRiR2xrV0ZKd1lqSTBhVTl1ZERsbVVUMDlJaXdpVW05MWJtUkRhR0Z1WjJWS2RYTjBhV1pwWTJGMGFXOXVJanB1ZFd4c0xDSlFjbVZ3WVhKbFNuVnpkR2xtYVdOaGRHbHZiaUk2Ym5Wc2JIMD0ifX0=\"\n         },\n         {\n            \"MsgType\":0,\n            \"MsgID\":[\n               142,\n               128,\n               6,\n               101,\n               81,\n               168,\n               27,\n               49,\n               130,\n               88,\n               112,\n               158,\n               218,\n               247,\n               221,\n               31,\n               99,\n               205,\n               104,\n               106,\n               14,\n               77,\n               184,\n               178,\n               155,\n               187,\n               122,\n               207,\n               230,\n               86,\n               8,\n               103,\n               122,\n               245,\n               165,\n               39,\n               217,\n               68,\n               142,\n               228,\n               120,\n               53,\n               72,\n               94,\n               2,\n               181,\n               11,\n               192,\n               0,\n               0,\n               0,\n               0\n            ],\n            \"Data\":\"eyJTaWduYXR1cmUiOiJ0ZE1Qd2xqZTdIaklRUTFIVkRTTVpSNENpWk9DdzVjZ05XUjYvdUJvZWExN1pJSzYramFCdHZLSFdYV3ZWdHQ0QTZXQzJibHVCaE5CUVRYVjFEdVZMN3V2K1dpdWpjRjZ4RUdTU0JEL2NlSmY3a2hwaEd4TVhjakU0cll2UjVkZCIsIlNpZ25lcnMiOlsxXSwiTWVzc2FnZSI6eyJNc2dUeXBlIjoxLCJIZWlnaHQiOjAsIlJvdW5kIjoxLCJJZGVudGlmaWVyIjoiam9BR1pWR29HekdDV0hDZTJ2ZmRIMlBOYUdvT1RiaXltN3Q2eitaV0NHZDY5YVVuMlVTTzVIZzFTRjRDdFF2QUFBQUFBQT09IiwiRGF0YSI6ImV5SkVZWFJoSWpvaVpYbEtSV1JZVWpWSmFuQTNTV3hTTldOSFZXbFBha0Z6U1d4Q01WbHJkR3hsVTBrMlYzcEZNRTFwZDNoTmFtZHpUbWwzZUUxRVJYTlBSRVZ6VFZSWk5FeEVTVE5NUkZFMVRFUkZlazFEZHpSUFEzZDRUVlJKYzAxVVZUUk1SRWw0VDBOM2VVNUVZM05OYWtsNFRFUk5lRXhFYXpWTVJFbDNUbE4zZUUxRVVYTk5WRUV5VEVSRk1FeEVZek5NUkVVMFRrTjNlRTU2WjNOTlZGVXhURVJGTkU1NWQzaE5ha2x6VFdwQk0weEVTWHBOUTNjMFRtbDNORXhFUlhkTmVYZDRUV3BKYzAxcVVURk1SRVV5VGxOM2VrOVRkM2xOVkdOelRtcG5jMDFVVVhsTVJFbDVUME4zZUUxcVFYTk9WRTF6VG5wSmMwOVVVWE5OYVhkNFQwUkZjMDFVUlhOTlZHdDVXRk4zYVZVeWVIWmtRMGsyVFZSSmMwbHNXbWhpUjJ4cldWaFNkbU5yYkhWYVIxWTBTV3B2ZUV4RFNrUmlNakYwWVZoU01GcFhWa3BpYlZKc1pVTkpOazE1ZDJsUk1qbDBZbGRzTUdSSFZteFVSMVoxV2pOU2IwbHFiM2hOYW1kelNXdE9kbUpYTVhCa1NGSnNXbGhPUW1SR1RuTmlNMUZwVDJwTk1reERTbGRaVjNod1drZEdNR0l6U2tSaU1qRjBZVmhTTUZwWFZrcGliVkpzWlVOSk5rMVVSamxNUTBwQ1pFaFNiR016VW1oa1IyeDJZbXRTYUdSSFJXbFBibk5wWXpKNGRtUkRTVFpKYWtWNVNXbDNhV0ZYTld0YVdHZHBUMmxKZWtscGQybFpiVlpvV1RJNWRWZ3lTbk5pTWs1eVdETktkbUl6VVdsUGFVbDNaVVJCZUUxRVNYZE5la0V3VFVSVmQwNXFRVE5OUkdkM1QxUkNhRTFFUlhkTmFrRjZUVVJSZDA1VVFUSk5SR04zVDBSQk5VMUhSWGROVkVGNVRVUk5kMDVFUVRGTlJGbDNUbnBCTkUxRWEzZFpWRUY0VFVSSmFVeERTbnBpTTFaNVdUSlZhVTl1YzJsYVdFSjJXVEpuYVU5cFNYZEphWGRwWTIwNWRtUkRTVFpKYWtJMFRVUkZkMDFxUVhwTlJGRjNUbFJCTWsxRVkzZFBSRUUxVFVkRmQwMVVRWGxOUkUxM1RrUkJNVTFFV1hkT2VrRTBUVVJyZDFsVVFYaE5SRWwzVFhwQk1FMUVWWGRPYWtFelRVUm5kMDlVUW1oTlJFVjNUV2xLT1V4RFNqQlpXRXB1V2xoUmFVOXVjMmxhV0VKMldUSm5hVTlwU1hoSmFYZHBZMjA1ZG1SRFNUWkpha0kwVFVSRmQwMXFRWHBOUkZGM1RsUkJNazFFWTNkUFJFRTFUVWRGZDAxVVFYbE5SRTEzVGtSQk1VMUVXWGRPZWtFMFRVUnJkMWxVUVhoTlJFbDNUWHBCTUUxRVZYZE9ha0V6VFVSbmQwOVVRbWhOUkVWM1RXbEtPV1pUZDJsUmJYaDJXVEowUlZsWVVtaEphbkIxWkZkNGMweERTa0phTW1SNVdsZGthR1JIVmtKaWJWSlJZMjA1ZGxwcFNUWmlibFp6WWtOM2FWVXpiSFZaTUU1MllsY3hjR1JJVW14YVZVcHpZakpPY2xWdE9YWmtRMGsyVjNwQmMwMURkM2RNUkVGelRVTjNkMHhFUVhOTlEzZDNURVJCYzAxRGQzZE1SRUZ6VFVOM2QweEVRWE5OUTNkM1RFUkJjMDFEZDNkTVJFRnpUVU4zZDB4RVFYTk5RM2QzVEVSQmMwMURkM2RNUkVGelRVWXdjMGxzVGpWaWJVNUVZakl4ZEdGWVVqQmFWMVpFWWpJMU1HTnRiR2xrV0ZKd1lqSTBhVTl1ZERsbVVUMDlJbjA9In19\"\n         },\n         {\n            \"MsgType\":0,\n            \"MsgID\":[\n               142,\n               128,\n               6,\n               101,\n               81,\n               168,\n               27,\n               49,\n               130,\n               88,\n               112,\n               158,\n               218,\n               247,\n               221,\n               31,\n               99,\n               205,\n               104,\n               106,\n               14,\n               77,\n               184,\n               178,\n               155,\n               187,\n               122,\n               207,\n               230,\n               86,\n               8,\n               103,\n               122,\n               245,\n               165,\n               39,\n               217,\n               68,\n               142,\n               228,\n               120,\n               53,\n               72,\n               94,\n               2,\n               181,\n               11,\n               192,\n               0,\n               0,\n               0,\n               0\n            ],\n            \"Data\":\"eyJTaWduYXR1cmUiOiJrV2JCQ055dGVnWExXNEU0VjZJYnIrcjI3TUFZVThJVkVQZDJGYmRxajlWK054N05OWFpkVDlsMUkvZlJWckxaQmdyN0dlQnptT1p6SWtYVFo1SWtDS1hBR3JEZStiL3AwNU5WUzVHTWJzbDdmeno1T0ZvVGFybyswbkdQRldtdiIsIlNpZ25lcnMiOlsyXSwiTWVzc2FnZSI6eyJNc2dUeXBlIjoxLCJIZWlnaHQiOjAsIlJvdW5kIjoxLCJJZGVudGlmaWVyIjoiam9BR1pWR29HekdDV0hDZTJ2ZmRIMlBOYUdvT1RiaXltN3Q2eitaV0NHZDY5YVVuMlVTTzVIZzFTRjRDdFF2QUFBQUFBQT09IiwiRGF0YSI6ImV5SkVZWFJoSWpvaVpYbEtSV1JZVWpWSmFuQTNTV3hTTldOSFZXbFBha0Z6U1d4Q01WbHJkR3hsVTBrMlYzcEZNRTFwZDNoTmFtZHpUbWwzZUUxRVJYTlBSRVZ6VFZSWk5FeEVTVE5NUkZFMVRFUkZlazFEZHpSUFEzZDRUVlJKYzAxVVZUUk1SRWw0VDBOM2VVNUVZM05OYWtsNFRFUk5lRXhFYXpWTVJFbDNUbE4zZUUxRVVYTk5WRUV5VEVSRk1FeEVZek5NUkVVMFRrTjNlRTU2WjNOTlZGVXhURVJGTkU1NWQzaE5ha2x6VFdwQk0weEVTWHBOUTNjMFRtbDNORXhFUlhkTmVYZDRUV3BKYzAxcVVURk1SRVV5VGxOM2VrOVRkM2xOVkdOelRtcG5jMDFVVVhsTVJFbDVUME4zZUUxcVFYTk9WRTF6VG5wSmMwOVVVWE5OYVhkNFQwUkZjMDFVUlhOTlZHdDVXRk4zYVZVeWVIWmtRMGsyVFZSSmMwbHNXbWhpUjJ4cldWaFNkbU5yYkhWYVIxWTBTV3B2ZUV4RFNrUmlNakYwWVZoU01GcFhWa3BpYlZKc1pVTkpOazE1ZDJsUk1qbDBZbGRzTUdSSFZteFVSMVoxV2pOU2IwbHFiM2hOYW1kelNXdE9kbUpYTVhCa1NGSnNXbGhPUW1SR1RuTmlNMUZwVDJwTk1reERTbGRaVjNod1drZEdNR0l6U2tSaU1qRjBZVmhTTUZwWFZrcGliVkpzWlVOSk5rMVVSamxNUTBwQ1pFaFNiR016VW1oa1IyeDJZbXRTYUdSSFJXbFBibk5wWXpKNGRtUkRTVFpKYWtWNVNXbDNhV0ZYTld0YVdHZHBUMmxKZWtscGQybFpiVlpvV1RJNWRWZ3lTbk5pTWs1eVdETktkbUl6VVdsUGFVbDNaVVJCZUUxRVNYZE5la0V3VFVSVmQwNXFRVE5OUkdkM1QxUkNhRTFFUlhkTmFrRjZUVVJSZDA1VVFUSk5SR04zVDBSQk5VMUhSWGROVkVGNVRVUk5kMDVFUVRGTlJGbDNUbnBCTkUxRWEzZFpWRUY0VFVSSmFVeERTbnBpTTFaNVdUSlZhVTl1YzJsYVdFSjJXVEpuYVU5cFNYZEphWGRwWTIwNWRtUkRTVFpKYWtJMFRVUkZkMDFxUVhwTlJGRjNUbFJCTWsxRVkzZFBSRUUxVFVkRmQwMVVRWGxOUkUxM1RrUkJNVTFFV1hkT2VrRTBUVVJyZDFsVVFYaE5SRWwzVFhwQk1FMUVWWGRPYWtFelRVUm5kMDlVUW1oTlJFVjNUV2xLT1V4RFNqQlpXRXB1V2xoUmFVOXVjMmxhV0VKMldUSm5hVTlwU1hoSmFYZHBZMjA1ZG1SRFNUWkpha0kwVFVSRmQwMXFRWHBOUkZGM1RsUkJNazFFWTNkUFJFRTFUVWRGZDAxVVFYbE5SRTEzVGtSQk1VMUVXWGRPZWtFMFRVUnJkMWxVUVhoTlJFbDNUWHBCTUUxRVZYZE9ha0V6VFVSbmQwOVVRbWhOUkVWM1RXbEtPV1pUZDJsUmJYaDJXVEowUlZsWVVtaEphbkIxWkZkNGMweERTa0phTW1SNVdsZGthR1JIVmtKaWJWSlJZMjA1ZGxwcFNUWmlibFp6WWtOM2FWVXpiSFZaTUU1MllsY3hjR1JJVW14YVZVcHpZakpPY2xWdE9YWmtRMGsyVjNwQmMwMURkM2RNUkVGelRVTjNkMHhFUVhOTlEzZDNURVJCYzAxRGQzZE1SRUZ6VFVOM2QweEVRWE5OUTNkM1RFUkJjMDFEZDNkTVJFRnpUVU4zZDB4RVFYTk5RM2QzVEVSQmMwMURkM2RNUkVGelRVWXdjMGxzVGpWaWJVNUVZakl4ZEdGWVVqQmFWMVpFWWpJMU1HTnRiR2xrV0ZKd1lqSTBhVTl1ZERsbVVUMDlJbjA9In19\"\n         },\n         {\n            \"MsgType\":0,\n            \"MsgID\":[\n               142,\n               128,\n               6,\n               101,\n               81,\n               168,\n               27,\n               49,\n               130,\n               88,\n               112,\n               158,\n               218,\n               247,\n               221,\n               31,\n               99,\n               205,\n               104,\n               106,\n               14,\n               77,\n               184,\n               178,\n               155,\n               187,\n               122,\n               207,\n               230,\n               86,\n               8,\n               103,\n               122,\n               245,\n               165,\n               39,\n               217,\n               68,\n               142,\n               228,\n               120,\n               53,\n               72,\n               94,\n               2,\n               181,\n               11,\n               192,\n               0,\n               0,\n               0,\n               0\n            ],\n            \"Data\":\"eyJTaWduYXR1cmUiOiJpSGRJRFhVM1F6Mk9VVFJWZjczR2sxd0NlM24wMm5ITk9TUG43RDFHVzFXaWZEc0toSU1aY2Q2ajJUV1F5MDlNQyswcG95MlBSeU1KcHRiREVtVEZwUW03emhpQXd5VXoyZEtGRzgvN01NT1FXdU1JaU9uQUs2K1JpS1FwOTVxLyIsIlNpZ25lcnMiOlszXSwiTWVzc2FnZSI6eyJNc2dUeXBlIjoxLCJIZWlnaHQiOjAsIlJvdW5kIjoxLCJJZGVudGlmaWVyIjoiam9BR1pWR29HekdDV0hDZTJ2ZmRIMlBOYUdvT1RiaXltN3Q2eitaV0NHZDY5YVVuMlVTTzVIZzFTRjRDdFF2QUFBQUFBQT09IiwiRGF0YSI6ImV5SkVZWFJoSWpvaVpYbEtSV1JZVWpWSmFuQTNTV3hTTldOSFZXbFBha0Z6U1d4Q01WbHJkR3hsVTBrMlYzcEZNRTFwZDNoTmFtZHpUbWwzZUUxRVJYTlBSRVZ6VFZSWk5FeEVTVE5NUkZFMVRFUkZlazFEZHpSUFEzZDRUVlJKYzAxVVZUUk1SRWw0VDBOM2VVNUVZM05OYWtsNFRFUk5lRXhFYXpWTVJFbDNUbE4zZUUxRVVYTk5WRUV5VEVSRk1FeEVZek5NUkVVMFRrTjNlRTU2WjNOTlZGVXhURVJGTkU1NWQzaE5ha2x6VFdwQk0weEVTWHBOUTNjMFRtbDNORXhFUlhkTmVYZDRUV3BKYzAxcVVURk1SRVV5VGxOM2VrOVRkM2xOVkdOelRtcG5jMDFVVVhsTVJFbDVUME4zZUUxcVFYTk9WRTF6VG5wSmMwOVVVWE5OYVhkNFQwUkZjMDFVUlhOTlZHdDVXRk4zYVZVeWVIWmtRMGsyVFZSSmMwbHNXbWhpUjJ4cldWaFNkbU5yYkhWYVIxWTBTV3B2ZUV4RFNrUmlNakYwWVZoU01GcFhWa3BpYlZKc1pVTkpOazE1ZDJsUk1qbDBZbGRzTUdSSFZteFVSMVoxV2pOU2IwbHFiM2hOYW1kelNXdE9kbUpYTVhCa1NGSnNXbGhPUW1SR1RuTmlNMUZwVDJwTk1reERTbGRaVjNod1drZEdNR0l6U2tSaU1qRjBZVmhTTUZwWFZrcGliVkpzWlVOSk5rMVVSamxNUTBwQ1pFaFNiR016VW1oa1IyeDJZbXRTYUdSSFJXbFBibk5wWXpKNGRtUkRTVFpKYWtWNVNXbDNhV0ZYTld0YVdHZHBUMmxKZWtscGQybFpiVlpvV1RJNWRWZ3lTbk5pTWs1eVdETktkbUl6VVdsUGFVbDNaVVJCZUUxRVNYZE5la0V3VFVSVmQwNXFRVE5OUkdkM1QxUkNhRTFFUlhkTmFrRjZUVVJSZDA1VVFUSk5SR04zVDBSQk5VMUhSWGROVkVGNVRVUk5kMDVFUVRGTlJGbDNUbnBCTkUxRWEzZFpWRUY0VFVSSmFVeERTbnBpTTFaNVdUSlZhVTl1YzJsYVdFSjJXVEpuYVU5cFNYZEphWGRwWTIwNWRtUkRTVFpKYWtJMFRVUkZkMDFxUVhwTlJGRjNUbFJCTWsxRVkzZFBSRUUxVFVkRmQwMVVRWGxOUkUxM1RrUkJNVTFFV1hkT2VrRTBUVVJyZDFsVVFYaE5SRWwzVFhwQk1FMUVWWGRPYWtFelRVUm5kMDlVUW1oTlJFVjNUV2xLT1V4RFNqQlpXRXB1V2xoUmFVOXVjMmxhV0VKMldUSm5hVTlwU1hoSmFYZHBZMjA1ZG1SRFNUWkpha0kwVFVSRmQwMXFRWHBOUkZGM1RsUkJNazFFWTNkUFJFRTFUVWRGZDAxVVFYbE5SRTEzVGtSQk1VMUVXWGRPZWtFMFRVUnJkMWxVUVhoTlJFbDNUWHBCTUUxRVZYZE9ha0V6VFVSbmQwOVVRbWhOUkVWM1RXbEtPV1pUZDJsUmJYaDJXVEowUlZsWVVtaEphbkIxWkZkNGMweERTa0phTW1SNVdsZGthR1JIVmtKaWJWSlJZMjA1ZGxwcFNUWmlibFp6WWtOM2FWVXpiSFZaTUU1MllsY3hjR1JJVW14YVZVcHpZakpPY2xWdE9YWmtRMGsyVjNwQmMwMURkM2RNUkVGelRVTjNkMHhFUVhOTlEzZDNURVJCYzAxRGQzZE1SRUZ6VFVOM2QweEVRWE5OUTNkM1RFUkJjMDFEZDNkTVJFRnpUVU4zZDB4RVFYTk5RM2QzVEVSQmMwMURkM2RNUkVGelRVWXdjMGxzVGpWaWJVNUVZakl4ZEdGWVVqQmFWMVpFWWpJMU1HTnRiR2xrV0ZKd1lqSTBhVTl1ZERsbVVUMDlJbjA9In19\"\n         },\n         {\n            \"MsgType\":0,\n            \"MsgID\":[\n               142,\n               128,\n               6,\n               101,\n               81,\n               168,\n               27,\n               49,\n               130,\n               88,\n               112,\n               158,\n               218,\n               247,\n               221,\n               31,\n               99,\n               205,\n               104,\n               106,\n               14,\n               77,\n               184,\n               178,\n               155,\n               187,\n               122,\n               207,\n               230,\n               86,\n               8,\n               103,\n               122,\n               245,\n               165,\n               39,\n               217,\n               68,\n               142,\n               228,\n               120,\n               53,\n               72,\n               94,\n               2,\n               181,\n               11,\n               192,\n               0,\n               0,\n               0,\n               0\n            ],\n            \"Data\":\"eyJTaWduYXR1cmUiOiJqL1BjOC83QmIvWVB2eUVDcWtEdEtnTUxObmtGMmxpd1YrWDJ0a0sydWcrTUdweGRyY1NodlN4dC9OaXhuTnBZRTJpODEzOE1Rc3lBcXJrMTZmczBrMy9QcUtpUFlJem8vS2U1YXpRN1p6MElHYWxNQlY2UWZVSUNlaEZjTXhaUCIsIlNpZ25lcnMiOlsxXSwiTWVzc2FnZSI6eyJNc2dUeXBlIjoyLCJIZWlnaHQiOjAsIlJvdW5kIjoxLCJJZGVudGlmaWVyIjoiam9BR1pWR29HekdDV0hDZTJ2ZmRIMlBOYUdvT1RiaXltN3Q2eitaV0NHZDY5YVVuMlVTTzVIZzFTRjRDdFF2QUFBQUFBQT09IiwiRGF0YSI6ImV5SkVZWFJoSWpvaVpYbEtSV1JZVWpWSmFuQTNTV3hTTldOSFZXbFBha0Z6U1d4Q01WbHJkR3hsVTBrMlYzcEZNRTFwZDNoTmFtZHpUbWwzZUUxRVJYTlBSRVZ6VFZSWk5FeEVTVE5NUkZFMVRFUkZlazFEZHpSUFEzZDRUVlJKYzAxVVZUUk1SRWw0VDBOM2VVNUVZM05OYWtsNFRFUk5lRXhFYXpWTVJFbDNUbE4zZUUxRVVYTk5WRUV5VEVSRk1FeEVZek5NUkVVMFRrTjNlRTU2WjNOTlZGVXhURVJGTkU1NWQzaE5ha2x6VFdwQk0weEVTWHBOUTNjMFRtbDNORXhFUlhkTmVYZDRUV3BKYzAxcVVURk1SRVV5VGxOM2VrOVRkM2xOVkdOelRtcG5jMDFVVVhsTVJFbDVUME4zZUUxcVFYTk9WRTF6VG5wSmMwOVVVWE5OYVhkNFQwUkZjMDFVUlhOTlZHdDVXRk4zYVZVeWVIWmtRMGsyVFZSSmMwbHNXbWhpUjJ4cldWaFNkbU5yYkhWYVIxWTBTV3B2ZUV4RFNrUmlNakYwWVZoU01GcFhWa3BpYlZKc1pVTkpOazE1ZDJsUk1qbDBZbGRzTUdSSFZteFVSMVoxV2pOU2IwbHFiM2hOYW1kelNXdE9kbUpYTVhCa1NGSnNXbGhPUW1SR1RuTmlNMUZwVDJwTk1reERTbGRaVjNod1drZEdNR0l6U2tSaU1qRjBZVmhTTUZwWFZrcGliVkpzWlVOSk5rMVVSamxNUTBwQ1pFaFNiR016VW1oa1IyeDJZbXRTYUdSSFJXbFBibk5wWXpKNGRtUkRTVFpKYWtWNVNXbDNhV0ZYTld0YVdHZHBUMmxKZWtscGQybFpiVlpvV1RJNWRWZ3lTbk5pTWs1eVdETktkbUl6VVdsUGFVbDNaVVJCZUUxRVNYZE5la0V3VFVSVmQwNXFRVE5OUkdkM1QxUkNhRTFFUlhkTmFrRjZUVVJSZDA1VVFUSk5SR04zVDBSQk5VMUhSWGROVkVGNVRVUk5kMDVFUVRGTlJGbDNUbnBCTkUxRWEzZFpWRUY0VFVSSmFVeERTbnBpTTFaNVdUSlZhVTl1YzJsYVdFSjJXVEpuYVU5cFNYZEphWGRwWTIwNWRtUkRTVFpKYWtJMFRVUkZkMDFxUVhwTlJGRjNUbFJCTWsxRVkzZFBSRUUxVFVkRmQwMVVRWGxOUkUxM1RrUkJNVTFFV1hkT2VrRTBUVVJyZDFsVVFYaE5SRWwzVFhwQk1FMUVWWGRPYWtFelRVUm5kMDlVUW1oTlJFVjNUV2xLT1V4RFNqQlpXRXB1V2xoUmFVOXVjMmxhV0VKMldUSm5hVTlwU1hoSmFYZHBZMjA1ZG1SRFNUWkpha0kwVFVSRmQwMXFRWHBOUkZGM1RsUkJNazFFWTNkUFJFRTFUVWRGZDAxVVFYbE5SRTEzVGtSQk1VMUVXWGRPZWtFMFRVUnJkMWxVUVhoTlJFbDNUWHBCTUUxRVZYZE9ha0V6VFVSbmQwOVVRbWhOUkVWM1RXbEtPV1pUZDJsUmJYaDJXVEowUlZsWVVtaEphbkIxWkZkNGMweERTa0phTW1SNVdsZGthR1JIVmtKaWJWSlJZMjA1ZGxwcFNUWmlibFp6WWtOM2FWVXpiSFZaTUU1MllsY3hjR1JJVW14YVZVcHpZakpPY2xWdE9YWmtRMGsyVjNwQmMwMURkM2RNUkVGelRVTjNkMHhFUVhOTlEzZDNURVJCYzAxRGQzZE1SRUZ6VFVOM2QweEVRWE5OUTNkM1RFUkJjMDFEZDNkTVJFRnpUVU4zZDB4RVFYTk5RM2QzVEVSQmMwMURkM2RNUkVGelRVWXdjMGxzVGpWaWJVNUVZakl4ZEdGWVVqQmFWMVpFWWpJMU1HTnRiR2xrV0ZKd1lqSTBhVTl1ZERsbVVUMDlJbjA9In19\"\n         },\n         {\n            \"MsgType\":0,\n            \"MsgID\":[\n               142,\n               128,\n               6,\n               101,\n               81,\n               168,\n               27,\n               49,\n               130,\n               88,\n               112,\n               158,\n               218,\n               247,\n               221,\n               31,\n               99,\n               205,\n               104,\n               106,\n               14,\n               77,\n               184,\n               178,\n               155,\n               187,\n               122,\n               207,\n               230,\n               86,\n               8,\n               103,\n               122,\n               245,\n               165,\n               39,\n               217,\n               68,\n               142,\n               228,\n               120,\n               53,\n               72,\n               94,\n               2,\n               181,\n               11,\n               192,\n               0,\n               0,\n               0,\n               0\n            ],\n            \"Data\":\"eyJTaWduYXR1cmUiOiJoZE5kNlJLTi9PaFl6REJPb1lHUUVzdzZ4TU5hMDhjdThvdkwxbG1PY3ZYTk1IRmp3N1JiLy9CNGpVS1hzTURCRmpRbXUzQzRYTXFGM09EYjBKTmxjU1d5Y0Z5KzNMemtNcnhubW8wcmhHRGtXWXNtbStzU3MvQmxrNWdEN0huZSIsIlNpZ25lcnMiOlsyXSwiTWVzc2FnZSI6eyJNc2dUeXBlIjoyLCJIZWlnaHQiOjAsIlJvdW5kIjoxLCJJZGVudGlmaWVyIjoiam9BR1pWR29HekdDV0hDZTJ2ZmRIMlBOYUdvT1RiaXltN3Q2eitaV0NHZDY5YVVuMlVTTzVIZzFTRjRDdFF2QUFBQUFBQT09IiwiRGF0YSI6ImV5SkVZWFJoSWpvaVpYbEtSV1JZVWpWSmFuQTNTV3hTTldOSFZXbFBha0Z6U1d4Q01WbHJkR3hsVTBrMlYzcEZNRTFwZDNoTmFtZHpUbWwzZUUxRVJYTlBSRVZ6VFZSWk5FeEVTVE5NUkZFMVRFUkZlazFEZHpSUFEzZDRUVlJKYzAxVVZUUk1SRWw0VDBOM2VVNUVZM05OYWtsNFRFUk5lRXhFYXpWTVJFbDNUbE4zZUUxRVVYTk5WRUV5VEVSRk1FeEVZek5NUkVVMFRrTjNlRTU2WjNOTlZGVXhURVJGTkU1NWQzaE5ha2x6VFdwQk0weEVTWHBOUTNjMFRtbDNORXhFUlhkTmVYZDRUV3BKYzAxcVVURk1SRVV5VGxOM2VrOVRkM2xOVkdOelRtcG5jMDFVVVhsTVJFbDVUME4zZUUxcVFYTk9WRTF6VG5wSmMwOVVVWE5OYVhkNFQwUkZjMDFVUlhOTlZHdDVXRk4zYVZVeWVIWmtRMGsyVFZSSmMwbHNXbWhpUjJ4cldWaFNkbU5yYkhWYVIxWTBTV3B2ZUV4RFNrUmlNakYwWVZoU01GcFhWa3BpYlZKc1pVTkpOazE1ZDJsUk1qbDBZbGRzTUdSSFZteFVSMVoxV2pOU2IwbHFiM2hOYW1kelNXdE9kbUpYTVhCa1NGSnNXbGhPUW1SR1RuTmlNMUZwVDJwTk1reERTbGRaVjNod1drZEdNR0l6U2tSaU1qRjBZVmhTTUZwWFZrcGliVkpzWlVOSk5rMVVSamxNUTBwQ1pFaFNiR016VW1oa1IyeDJZbXRTYUdSSFJXbFBibk5wWXpKNGRtUkRTVFpKYWtWNVNXbDNhV0ZYTld0YVdHZHBUMmxKZWtscGQybFpiVlpvV1RJNWRWZ3lTbk5pTWs1eVdETktkbUl6VVdsUGFVbDNaVVJCZUUxRVNYZE5la0V3VFVSVmQwNXFRVE5OUkdkM1QxUkNhRTFFUlhkTmFrRjZUVVJSZDA1VVFUSk5SR04zVDBSQk5VMUhSWGROVkVGNVRVUk5kMDVFUVRGTlJGbDNUbnBCTkUxRWEzZFpWRUY0VFVSSmFVeERTbnBpTTFaNVdUSlZhVTl1YzJsYVdFSjJXVEpuYVU5cFNYZEphWGRwWTIwNWRtUkRTVFpKYWtJMFRVUkZkMDFxUVhwTlJGRjNUbFJCTWsxRVkzZFBSRUUxVFVkRmQwMVVRWGxOUkUxM1RrUkJNVTFFV1hkT2VrRTBUVVJyZDFsVVFYaE5SRWwzVFhwQk1FMUVWWGRPYWtFelRVUm5kMDlVUW1oTlJFVjNUV2xLT1V4RFNqQlpXRXB1V2xoUmFVOXVjMmxhV0VKMldUSm5hVTlwU1hoSmFYZHBZMjA1ZG1SRFNUWkpha0kwVFVSRmQwMXFRWHBOUkZGM1RsUkJNazFFWTNkUFJFRTFUVWRGZDAxVVFYbE5SRTEzVGtSQk1VMUVXWGRPZWtFMFRVUnJkMWxVUVhoTlJFbDNUWHBCTUUxRVZYZE9ha0V6VFVSbmQwOVVRbWhOUkVWM1RXbEtPV1pUZDJsUmJYaDJXVEowUlZsWVVtaEphbkIxWkZkNGMweERTa0phTW1SNVdsZGthR1JIVmtKaWJWSlJZMjA1ZGxwcFNUWmlibFp6WWtOM2FWVXpiSFZaTUU1MllsY3hjR1JJVW14YVZVcHpZakpPY2xWdE9YWmtRMGsyVjNwQmMwMURkM2RNUkVGelRVTjNkMHhFUVhOTlEzZDNURVJCYzAxRGQzZE1SRUZ6VFVOM2QweEVRWE5OUTNkM1RFUkJjMDFEZDNkTVJFRnpUVU4zZDB4RVFYTk5RM2QzVEVSQmMwMURkM2RNUkVGelRVWXdjMGxzVGpWaWJVNUVZakl4ZEdGWVVqQmFWMVpFWWpJMU1HTnRiR2xrV0ZKd1lqSTBhVTl1ZERsbVVUMDlJbjA9In19\"\n         },\n         {\n            \"MsgType\":0,\n            \"MsgID\":[\n               142,\n               128,\n               6,\n               101,\n               81,\n               168,\n               27,\n               49,\n               130,\n               88,\n               112,\n               158,\n               218,\n               247,\n               221,\n               31,\n               99,\n               205,\n               104,\n               106,\n               14,\n               77,\n               184,\n               178,\n               155,\n               187,\n               122,\n               207,\n               230,\n               86,\n               8,\n               103,\n               122,\n               245,\n               165,\n               39,\n               217,\n               68,\n               142,\n               228,\n               120,\n               53,\n               72,\n               94,\n               2,\n               181,\n               11,\n               192,\n               0,\n               0,\n               0,\n               0\n            ],\n            \"Data\":\"eyJTaWduYXR1cmUiOiJrYzE2bVJ4WSsvNDJhWVpScjJ0eThlQ05ZTjZ0OFhzMVY4SjFLUFE2Y0JxclBYNENMd09TV3BzZzdpUVQvQm5rR014Rk9XSU1mTUhKUWc0ejZFN0dvN2NrNzBuQ0tIVi9RVkcxVWlOTy9iMTJTYTlhemFoYk01TFR4UDU5Y2dvUSIsIlNpZ25lcnMiOlszXSwiTWVzc2FnZSI6eyJNc2dUeXBlIjoyLCJIZWlnaHQiOjAsIlJvdW5kIjoxLCJJZGVudGlmaWVyIjoiam9BR1pWR29HekdDV0hDZTJ2ZmRIMlBOYUdvT1RiaXltN3Q2eitaV0NHZDY5YVVuMlVTTzVIZzFTRjRDdFF2QUFBQUFBQT09IiwiRGF0YSI6ImV5SkVZWFJoSWpvaVpYbEtSV1JZVWpWSmFuQTNTV3hTTldOSFZXbFBha0Z6U1d4Q01WbHJkR3hsVTBrMlYzcEZNRTFwZDNoTmFtZHpUbWwzZUUxRVJYTlBSRVZ6VFZSWk5FeEVTVE5NUkZFMVRFUkZlazFEZHpSUFEzZDRUVlJKYzAxVVZUUk1SRWw0VDBOM2VVNUVZM05OYWtsNFRFUk5lRXhFYXpWTVJFbDNUbE4zZUUxRVVYTk5WRUV5VEVSRk1FeEVZek5NUkVVMFRrTjNlRTU2WjNOTlZGVXhURVJGTkU1NWQzaE5ha2x6VFdwQk0weEVTWHBOUTNjMFRtbDNORXhFUlhkTmVYZDRUV3BKYzAxcVVURk1SRVV5VGxOM2VrOVRkM2xOVkdOelRtcG5jMDFVVVhsTVJFbDVUME4zZUUxcVFYTk9WRTF6VG5wSmMwOVVVWE5OYVhkNFQwUkZjMDFVUlhOTlZHdDVXRk4zYVZVeWVIWmtRMGsyVFZSSmMwbHNXbWhpUjJ4cldWaFNkbU5yYkhWYVIxWTBTV3B2ZUV4RFNrUmlNakYwWVZoU01GcFhWa3BpYlZKc1pVTkpOazE1ZDJsUk1qbDBZbGRzTUdSSFZteFVSMVoxV2pOU2IwbHFiM2hOYW1kelNXdE9kbUpYTVhCa1NGSnNXbGhPUW1SR1RuTmlNMUZwVDJwTk1reERTbGRaVjNod1drZEdNR0l6U2tSaU1qRjBZVmhTTUZwWFZrcGliVkpzWlVOSk5rMVVSamxNUTBwQ1pFaFNiR016VW1oa1IyeDJZbXRTYUdSSFJXbFBibk5wWXpKNGRtUkRTVFpKYWtWNVNXbDNhV0ZYTld0YVdHZHBUMmxKZWtscGQybFpiVlpvV1RJNWRWZ3lTbk5pTWs1eVdETktkbUl6VVdsUGFVbDNaVVJCZUUxRVNYZE5la0V3VFVSVmQwNXFRVE5OUkdkM1QxUkNhRTFFUlhkTmFrRjZUVVJSZDA1VVFUSk5SR04zVDBSQk5VMUhSWGROVkVGNVRVUk5kMDVFUVRGTlJGbDNUbnBCTkUxRWEzZFpWRUY0VFVSSmFVeERTbnBpTTFaNVdUSlZhVTl1YzJsYVdFSjJXVEpuYVU5cFNYZEphWGRwWTIwNWRtUkRTVFpKYWtJMFRVUkZkMDFxUVhwTlJGRjNUbFJCTWsxRVkzZFBSRUUxVFVkRmQwMVVRWGxOUkUxM1RrUkJNVTFFV1hkT2VrRTBUVVJyZDFsVVFYaE5SRWwzVFhwQk1FMUVWWGRPYWtFelRVUm5kMDlVUW1oTlJFVjNUV2xLT1V4RFNqQlpXRXB1V2xoUmFVOXVjMmxhV0VKMldUSm5hVTlwU1hoSmFYZHBZMjA1ZG1SRFNUWkpha0kwVFVSRmQwMXFRWHBOUkZGM1RsUkJNazFFWTNkUFJFRTFUVWRGZDAxVVFYbE5SRTEzVGtSQk1VMUVXWGRPZWtFMFRVUnJkMWxVUVhoTlJFbDNUWHBCTUUxRVZYZE9ha0V6VFVSbmQwOVVRbWhOUkVWM1RXbEtPV1pUZDJsUmJYaDJXVEowUlZsWVVtaEphbkIxWkZkNGMweERTa0phTW1SNVdsZGthR1JIVmtKaWJWSlJZMjA1ZGxwcFNUWmlibFp6WWtOM2FWVXpiSFZaTUU1MllsY3hjR1JJVW14YVZVcHpZakpPY2xWdE9YWmtRMGsyVjNwQmMwMURkM2RNUkVGelRVTjNkMHhFUVhOTlEzZDNURVJCYzAxRGQzZE1SRUZ6VFVOM2QweEVRWE5OUTNkM1RFUkJjMDFEZDNkTVJFRnpUVU4zZDB4RVFYTk5RM2QzVEVSQmMwMURkM2RNUkVGelRVWXdjMGxzVGpWaWJVNUVZakl4ZEdGWVVqQmFWMVpFWWpJMU1HTnRiR2xrV0ZKd1lqSTBhVTl1ZERsbVVUMDlJbjA9In19\"\n         },\n         {\n            \"MsgType\":2,\n            \"MsgID\":[\n               142,\n               128,\n               6,\n               101,\n               81,\n               168,\n               27,\n               49,\n               130,\n               88,\n               112,\n               158,\n               218,\n               247,\n               221,\n               31,\n               99,\n               205,\n               104,\n               106,\n               14,\n               77,\n               184,\n               178,\n               155,\n               187,\n               122,\n               207,\n               230,\n               86,\n               8,\n               103,\n               122,\n               245,\n               165,\n               39,\n               217,\n               68,\n               142,\n               228,\n               120,\n               53,\n               72,\n               94,\n               2,\n               181,\n               11,\n               192,\n               0,\n               0,\n               0,\n               0\n            ],\n            \"Data\":\"eyJUeXBlIjowLCJNZXNzYWdlcyI6W3siU2xvdCI6MTIsIlBhcnRpYWxTaWduYXR1cmUiOiJyK3VKQm51ZldDR3lYQ0dHV3QwcW1hMURlVzhJRk1BLzZuUW9OQ2JhZ21ERGtPS2pvYm9HUWFpUW9NQnhYb3Z1RlRJdVRyelA0T2ZaWmJJb0hTcmlGYzc2SUpmMXpjUHBGNnBTOU1RS2xRd1ZJYWhmaEpYcG5WNit5Lzg5eFdlaSIsIlNpZ25pbmdSb290IjoiZ1VVY1dMQjV4YStFNitTNUtRRFQ2Y1dqUm1lTXR0dzhTMzdxTEp5elZsOD0iLCJTaWduZXJzIjpbMV0sIk1ldGFEYXRhIjpudWxsfV0sIlNpZ25hdHVyZSI6Imc2WmszMGhoUEpSaVlFbnFpQUhYZUpSeXFaTjUyT0MrWUVNRUVUN3FSMlFIK1RhaE5TemlCWGxSYjlDQjFjc0hCemhkYk5na1h5d3VUMUVVdjdKa1Rqdk9YUzFKbFNRNnlqQnVRbmFISVF2aEtYWHZQc1JwWmxWQXRGMm5xdlNSIiwiU2lnbmVycyI6WzFdfQ==\"\n         },\n         {\n            \"MsgType\":2,\n            \"MsgID\":[\n               142,\n               128,\n               6,\n               101,\n               81,\n               168,\n               27,\n               49,\n               130,\n               88,\n               112,\n               158,\n               218,\n               247,\n               221,\n               31,\n               99,\n               205,\n               104,\n               106,\n               14,\n               77,\n               184,\n               178,\n               155,\n               187,\n               122,\n               207,\n               230,\n               86,\n               8,\n               103,\n               122,\n               245,\n               165,\n               39,\n               217,\n               68,\n               142,\n               228,\n               120,\n               53,\n               72,\n               94,\n               2,\n               181,\n               11,\n               192,\n               0,\n               0,\n               0,\n               0\n            ],\n            \"Data\":\"eyJUeXBlIjowLCJNZXNzYWdlcyI6W3siU2xvdCI6MTIsIlBhcnRpYWxTaWduYXR1cmUiOiJoai80SUNBcko5SWJtdHBOMElzUlE5M0FlN3VxY2xobG41bzRCaEdrV3pFWWQ1NFNHM2c0dndhenA0VWIyQVRFRmEvcm9TdGMyaE1adVhMV1lhOWJudk9QWHFFdkluREk4Y012U2w0MnRGMVFGVEo5dmY1Q2dkc2QyNWJPYWJyRyIsIlNpZ25pbmdSb290IjoiZ1VVY1dMQjV4YStFNitTNUtRRFQ2Y1dqUm1lTXR0dzhTMzdxTEp5elZsOD0iLCJTaWduZXJzIjpbMl0sIk1ldGFEYXRhIjpudWxsfV0sIlNpZ25hdHVyZSI6ImcySHlUMDNuaG9zbXl6eEkvczIvL0o4R3owd1pvU29WK3J2SlYrTEpOVkdrYk1MS0RnbGJTYWNlNGpvY1drbkhHSE1sNGZmS2ZScEpYekF6bUlDU0xiK0F0NWZoQkpxRW9Oa1M3YXdYa3pWTWhTUVZYWlFBcDhHS2doOWt0NGx6IiwiU2lnbmVycyI6WzJdfQ==\"\n         },\n         {\n            \"MsgType\":2,\n            \"MsgID\":[\n               142,\n               128,\n               6,\n               101,\n               81,\n               168,\n               27,\n               49,\n               130,\n               88,\n               112,\n               158,\n               218,\n               247,\n               221,\n               31,\n               99,\n               205,\n               104,\n               106,\n               14,\n               77,\n               184,\n               178,\n               155,\n               187,\n               122,\n               207,\n               230,\n               86,\n               8,\n               103,\n               122,\n               245,\n               165,\n               39,\n               217,\n               68,\n               142,\n               228,\n               120,\n               53,\n               72,\n               94,\n               2,\n               181,\n               11,\n               192,\n               0,\n               0,\n               0,\n               0\n            ],\n            \"Data\":\"eyJUeXBlIjowLCJNZXNzYWdlcyI6W3siU2xvdCI6MTIsIlBhcnRpYWxTaWduYXR1cmUiOiJrbDAzekpTMzczcE16NlJLeDlNYllXbjA1RDd0ckhETFBsaDYrSVpVRGRnL1lDY05nZzc1bDY2WTdXTVUxejkrRGs5aWhpMXJIT011cmJncGJFeGE5YXNEdUJlbkdHSnU5RHZGWUFRSldvbUI2ZDRHWnFxWmVpaXpqSWVmSU9pYiIsIlNpZ25pbmdSb290IjoiZ1VVY1dMQjV4YStFNitTNUtRRFQ2Y1dqUm1lTXR0dzhTMzdxTEp5elZsOD0iLCJTaWduZXJzIjpbM10sIk1ldGFEYXRhIjpudWxsfV0sIlNpZ25hdHVyZSI6InNudzZZV1JkeUxBb2FZR2lIQndVLzN6OHdCWSs1d1pCcHdwK3pnSnJKNHFzcEQyYmJ3RlQ2QUl6Mm5STFlWaDVCQlhpVTBHcTZpcnROZFh2bWprTm9jL2Rta2FTbE1wcW1ROS9WcU9BUWt0OXhHSVAzRzUvQmNWWFdBNXJnWEl6IiwiU2lnbmVycyI6WzNdfQ==\"\n         }\n      ]")
	rootOutput, err := hex.DecodeString("5dfb3ba03010b92fcd7404dc1f8e7d43e0ebfcdbe63e46fec25c358d500eb26f")
	require.NoError(t, err)
	//shareInput := []byte("{\n            \"OperatorID\":1,\n            \"ValidatorPubKey\":\"joAGZVGoGzGCWHCe2vfdH2PNaGoOTbiym7t6z+ZWCGd69aUn2USO5Hg1SF4CtQvA\",\n            \"SharePubKey\":\"l9lKgR1kSTYFKp0tSs1kcYl0z2eNvv0mcyTI6fjnA0pKa32HeeJ6AZU4w8Qlw+Xn\",\n            \"Committee\":[\n               {\n                  \"OperatorID\":1,\n                  \"PubKey\":\"l9lKgR1kSTYFKp0tSs1kcYl0z2eNvv0mcyTI6fjnA0pKa32HeeJ6AZU4w8Qlw+Xn\"\n               },\n               {\n                  \"OperatorID\":2,\n                  \"PubKey\":\"przr4wl9dBcbQMcSoDHOsDcds9PEAs8s5pG5Eg87q3XU1W36DzdZFUSZm/GMU1Pt\"\n               },\n               {\n                  \"OperatorID\":3,\n                  \"PubKey\":\"gJDgt2ZqRezF1O90GKyZ8J5sskQCn+pqCn/Mvp7gi8U53g36Zr5rq8hJPdmd0amN\"\n               },\n               {\n                  \"OperatorID\":4,\n                  \"PubKey\":\"p8CidrcKXuM5XH1tJlXtYFKKolLU0h7KX8xSI+UMxCvRaLKAq3q1MXNU3d/PPfnk\"\n               }\n            ],\n            \"Quorum\":3,\n            \"PartialQuorum\":2,\n            \"DomainType\":\"cHJpbXVzX3Rlc3RuZXQ=\",\n            \"Graffiti\":null\n         }")

	logger := logex.GetLogger()

	ctx, cancel := context.WithCancel(context.TODO())
	pi, _ := protocolp2p.GenPeerID()
	p2pNet := protocolp2p.NewMockNetwork(logger, pi, 10)
	beacon := NewTestBeacon(t)
	network := beaconprotocol.NewNetwork(core.PraterNetwork)
	keysSet := testingutils.Testing4SharesSet()

	// keysSet.Shares[1].GetPublicKey().Serialize(),

	cfg := basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Ctx:    context.Background(),
	}
	db, err := storage.GetStorageFactory(cfg)
	require.NoError(t, err)
	ibftStorage := qbftStorage.New(db, logger, convertToSpecRole(message.RoleTypeAttester).String(), forksprotocol.V0ForkVersion)

	require.NoError(t, beacon.AddShare(keysSet.Shares[1]))
	share := &beaconprotocol.Share{
		NodeID:    1,
		PublicKey: keysSet.ValidatorPK,
		Committee: map[message.OperatorID]*beaconprotocol.Node{
			1: {
				IbftID: 1,
				Pk:     keysSet.Shares[1].GetPublicKey().Serialize(),
			},
			2: {
				IbftID: 2,
				Pk:     keysSet.Shares[2].GetPublicKey().Serialize(),
			},
			3: {
				IbftID: 3,
				Pk:     keysSet.Shares[3].GetPublicKey().Serialize(),
			},
			4: {
				IbftID: 4,
				Pk:     keysSet.Shares[4].GetPublicKey().Serialize(),
			},
		},
	}

	v := &Validator{
		ctx:         ctx,
		cancelCtx:   cancel,
		logger:      logger,
		network:     network,
		p2pNetwork:  p2pNet,
		beacon:      beacon,
		Share:       share,
		signer:      beacon,
		ibfts:       nil,
		readMode:    false,
		saveHistory: false,
	}

	attestCtrl := newQbftController(t, ctx, logger, message.RoleTypeAttester, ibftStorage, p2pNet, beacon, share, forksprotocol.V0ForkVersion, beacon)
	ibfts := make(map[message.RoleType]controller.IController)
	ibfts[message.RoleTypeAttester] = attestCtrl
	v.ibfts = ibfts

	var dutyPk [48]byte
	copy(dutyPk[:], keysSet.ValidatorPK.Serialize())

	duty := &beaconprotocol.Duty{
		Type:                    message.RoleTypeAttester,
		PubKey:                  dutyPk,
		Slot:                    12,
		ValidatorIndex:          1,
		CommitteeIndex:          3,
		CommitteeLength:         128,
		CommitteesAtSlot:        36,
		ValidatorCommitteeIndex: 11,
	}
	require.NoError(t, attestCtrl.Init())
	go v.ExecuteDuty(12, duty)

	for _, msg := range jsonToMsgs(t, msgsInput, message.NewIdentifier(share.PublicKey.Serialize(), message.RoleTypeAttester)) {
		require.NoError(t, v.ProcessMsg(msg))
	}

	time.Sleep(time.Second * 3) // 3s round

	// ------------- OUTPUT -------------------------------------------
	var mappedCommittee []*types.Operator
	for k, v := range share.Committee {
		mappedCommittee = append(mappedCommittee, &types.Operator{
			OperatorID: types.OperatorID(k),
			PubKey:     v.Pk,
		})
	}

	qbftCtrl := v.ibfts[message.RoleTypeAttester]
	currentInstance := qbftCtrl.GetCurrentInstance()
	mappedShare := &types.Share{
		OperatorID:      types.OperatorID(v.Share.NodeID),
		ValidatorPubKey: v.Share.PublicKey.Serialize(),
		SharePubKey:     v.Share.Committee[v.Share.NodeID].Pk,
		Committee:       mappedCommittee,
		Quorum:          3,
		PartialQuorum:   2,
		DomainType:      []byte("cHJpbXVzX3Rlc3RuZXQ="),
		Graffiti:        nil,
	}
	decided, err := ibftStorage.GetLastDecided(qbftCtrl.GetIdentifier())
	require.NoError(t, err)
	decidedValue := []byte("")
	if decided != nil {
		cd, err := decided.Message.GetCommitData()
		require.NoError(t, err)
		decidedValue = cd.Data
	}

	mappedInstance := new(qbft.Instance)
	if currentInstance != nil {
		mappedInstance.State = &qbft.State{
			Share:                           mappedShare,
			ID:                              qbftCtrl.GetIdentifier(),
			Round:                           qbft.Round(currentInstance.State().GetRound()),
			Height:                          qbft.Height(currentInstance.State().GetHeight()),
			LastPreparedRound:               qbft.Round(currentInstance.State().GetPreparedRound()),
			LastPreparedValue:               currentInstance.State().GetPreparedValue(),
			ProposalAcceptedForCurrentRound: nil,
			Decided:                         decided != nil && decided.Message.Height == currentInstance.State().GetHeight(), // TODO might need to add this flag to qbftCtrl
			DecidedValue:                    decidedValue,                                                                    // TODO allow a way to get it
			ProposeContainer:                convertToSpecContainer(t, currentInstance.Containers()[qbft.ProposalMsgType]),
			PrepareContainer:                convertToSpecContainer(t, currentInstance.Containers()[qbft.PrepareMsgType]),
			CommitContainer:                 convertToSpecContainer(t, currentInstance.Containers()[qbft.CommitMsgType]),
			RoundChangeContainer:            convertToSpecContainer(t, currentInstance.Containers()[qbft.RoundChangeMsgType]),
		}
		mappedInstance.StartValue = currentInstance.State().GetInputValue()
	}

	mappedDecidedValue := &types.ConsensusData{
		Duty: &types.Duty{
			Type:                    0,
			PubKey:                  spec.BLSPubKey{},
			Slot:                    0,
			ValidatorIndex:          0,
			CommitteeIndex:          0,
			CommitteeLength:         0,
			CommitteesAtSlot:        0,
			ValidatorCommitteeIndex: 0,
		},
		AttestationData:           nil,
		BlockData:                 nil,
		AggregateAndProof:         nil,
		SyncCommitteeBlockRoot:    spec.Root{},
		SyncCommitteeContribution: nil,
	}

	mappedSignedAtts := &spec.Attestation{
		AggregationBits: nil,
		Data:            nil,
		Signature:       spec.BLSSignature{},
	}

	resState := ssv.NewDutyExecutionState(3)
	resState.RunningInstance = mappedInstance
	resState.DecidedValue = mappedDecidedValue
	resState.SignedAttestation = mappedSignedAtts
	resState.Finished = true // TODO need to set real value

	root, err := resState.GetRoot()
	require.NoError(t, err)
	require.Equal(t, rootOutput, root) // TODO need to change based on the test
}

func convertToSpecRole(role message.RoleType) types.BeaconRole {
	switch role {
	case message.RoleTypeAttester:
		return types.BNRoleAttester
	case message.RoleTypeAggregator:
		return types.BNRoleAggregator
	case message.RoleTypeProposer:
		return types.BNRoleProposer
	default:
		panic(fmt.Sprintf("unknown role type! (%s)", role.String()))
	}
	return 0
}

func jsonToMsgs(t *testing.T, input []byte, identifier message.Identifier) []*message.SSVMessage {
	var msgs []*types.SSVMessage
	require.NoError(t, json.Unmarshal(input, &msgs))

	var res []*message.SSVMessage
	for _, msg := range msgs {
		data := msg.Data

		if msg.MsgType == types.SSVPartialSignatureMsgType {
			sps := new(ssv.SignedPartialSignatureMessage)
			require.NoError(t, sps.Decode(msg.Data))
			spsm := sps.Messages[0]
			spcm := &message.SignedPostConsensusMessage{
				Message: &message.PostConsensusMessage{
					Height:          0, // TODO need to get height fom ssv.SignedPartialSignatureMessage
					DutySignature:   spsm.PartialSignature,
					DutySigningRoot: spsm.SigningRoot,
					Signers:         convertSingers(spsm.Signers),
				},
				Signature: message.Signature(sps.Signature),
				Signers:   convertSingers(sps.Signers),
			}

			encoded, err := spcm.Encode()
			require.NoError(t, err)
			data = encoded
		}

		var msgType message.MsgType
		switch msg.GetType() {
		case types.SSVConsensusMsgType:
			msgType = message.SSVConsensusMsgType
		case types.SSVDecidedMsgType:
			msgType = message.SSVDecidedMsgType
		case types.SSVPartialSignatureMsgType:
			msgType = message.SSVPostConsensusMsgType
		case types.DKGMsgType:
			panic("type not supported yet")
		}
		sm := &message.SSVMessage{
			MsgType: msgType,
			ID:      identifier,
			Data:    data,
		}
		res = append(res, sm)
	}
	return res
}

func TestJsonToShare(t *testing.T) {
	require.NoError(t, bls.Init(bls.BLS12_381))
	shareInput := []byte("{\n            \"OperatorID\":1,\n            \"ValidatorPubKey\":\"joAGZVGoGzGCWHCe2vfdH2PNaGoOTbiym7t6z+ZWCGd69aUn2USO5Hg1SF4CtQvA\",\n            \"SharePubKey\":\"l9lKgR1kSTYFKp0tSs1kcYl0z2eNvv0mcyTI6fjnA0pKa32HeeJ6AZU4w8Qlw+Xn\",\n            \"Committee\":[\n               {\n                  \"OperatorID\":1,\n                  \"PubKey\":\"l9lKgR1kSTYFKp0tSs1kcYl0z2eNvv0mcyTI6fjnA0pKa32HeeJ6AZU4w8Qlw+Xn\"\n               },\n               {\n                  \"OperatorID\":2,\n                  \"PubKey\":\"przr4wl9dBcbQMcSoDHOsDcds9PEAs8s5pG5Eg87q3XU1W36DzdZFUSZm/GMU1Pt\"\n               },\n               {\n                  \"OperatorID\":3,\n                  \"PubKey\":\"gJDgt2ZqRezF1O90GKyZ8J5sskQCn+pqCn/Mvp7gi8U53g36Zr5rq8hJPdmd0amN\"\n               },\n               {\n                  \"OperatorID\":4,\n                  \"PubKey\":\"p8CidrcKXuM5XH1tJlXtYFKKolLU0h7KX8xSI+UMxCvRaLKAq3q1MXNU3d/PPfnk\"\n               }\n            ],\n            \"Quorum\":3,\n            \"PartialQuorum\":2,\n            \"DomainType\":\"cHJpbXVzX3Rlc3RuZXQ=\",\n            \"Graffiti\":null\n         }")
	jsonToShare(t, shareInput)
}
func jsonToShare(t *testing.T, input []byte) *beaconprotocol.Share { // TODO need to use spec parsing func in run_test.go
	committee := make(map[message.OperatorID]*beaconprotocol.Node)
	committee[1] = &beaconprotocol.Node{
		IbftID: 1,
		Pk:     []byte("l9lKgR1kSTYFKp0tSs1kcYl0z2eNvv0mcyTI6fjnA0pKa32HeeJ6AZU4w8Qlw+Xn"),
	}
	committee[2] = &beaconprotocol.Node{
		IbftID: 2,
		Pk:     []byte("przr4wl9dBcbQMcSoDHOsDcds9PEAs8s5pG5Eg87q3XU1W36DzdZFUSZm/GMU1Pt"),
	}
	committee[3] = &beaconprotocol.Node{
		IbftID: 3,
		Pk:     []byte("gJDgt2ZqRezF1O90GKyZ8J5sskQCn+pqCn/Mvp7gi8U53g36Zr5rq8hJPdmd0amN"),
	}
	committee[4] = &beaconprotocol.Node{
		IbftID: 4,
		Pk:     []byte("p8CidrcKXuM5XH1tJlXtYFKKolLU0h7KX8xSI+UMxCvRaLKAq3q1MXNU3d/PPfnk"),
	}

	return &beaconprotocol.Share{
		NodeID:    1,
		PublicKey: stringToBlsPubKey(t, "joAGZVGoGzGCWHCe2vfdH2PNaGoOTbiym7t6z+ZWCGd69aUn2USO5Hg1SF4CtQvA"),
		Committee: committee,
	}
}

func stringToBlsPubKey(t *testing.T, pubKeyString string) *bls.PublicKey {
	b, err := hex.DecodeString("95087182937f6982ae99f9b06bd116f463f414513032e33a3d175d9662eddf162101fcf6ca2a9fedaded74b8047c5dcf")
	require.NoError(t, err)
	pubKey := &bls.PublicKey{}
	require.NoError(t, pubKey.Deserialize(b))
	return pubKey
}

func newQbftController(t *testing.T, ctx context.Context, logger *zap.Logger, roleType message.RoleType, ibftStorage qbftstorage.QBFTStore, net protocolp2p.MockNetwork, beacon *TestBeacon, share *beaconprotocol.Share, version forksprotocol.ForkVersion, b *TestBeacon) *controller.Controller {
	logger = logger.With(zap.String("role", roleType.String()), zap.Bool("read mode", false))
	identifier := message.NewIdentifier(share.PublicKey.Serialize(), roleType)
	fork := forksfactory.NewFork(version)

	q, err := msgqueue.New(
		logger.With(zap.String("who", "msg_q")),
		msgqueue.WithIndexers( /*msgqueue.DefaultMsgIndexer(), */ msgqueue.SignedMsgIndexer(), msgqueue.DecidedMsgIndexer(), msgqueue.SignedPostConsensusMsgIndexer()),
	)
	require.NoError(t, err)

	ctrl := &controller.Controller{
		Ctx:                ctx,
		InstanceStorage:    ibftStorage,
		ChangeRoundStorage: ibftStorage,
		Logger:             logger,
		Network:            net,
		InstanceConfig:     qbft2.DefaultConsensusParams(),
		ValidatorShare:     share,
		Identifier:         identifier,
		Fork:               fork,
		Beacon:             beacon,
		Signer:             beacon,
		SignatureState:     controller.SignatureState{SignatureCollectionTimeout: time.Second * 5},

		SyncRateLimit: time.Second * 5,

		Q: q,

		CurrentInstanceLock: &sync.RWMutex{},
		ForkLock:            &sync.Mutex{},
		ReadMode:            false,
	}

	ctrl.DecidedFactory = factory.NewDecidedFactory(logger, ctrl.GetNodeMode(), ibftStorage, net)
	ctrl.DecidedStrategy = ctrl.DecidedFactory.GetStrategy()

	// set flags
	ctrl.State = controller.Ready

	go ctrl.StartQueueConsumer(ctrl.MessageHandler)

	return ctrl
}

func convertToSpecContainer(t *testing.T, container msgcont.MessageContainer) *qbft.MsgContainer {
	c := qbft.NewMsgContainer()
	container.AllMessaged(func(round message.Round, msg *message.SignedMessage) {
		var signers []types.OperatorID
		for _, s := range msg.GetSigners() {
			signers = append(signers, types.OperatorID(s))
		}

		// TODO need to use one of the message type (spec/protocol)
		ok, err := c.AddIfDoesntExist(&qbft.SignedMessage{
			Signature: types.Signature(msg.Signature),
			Signers:   signers,
			Message: &qbft.Message{
				MsgType:    qbft.MessageType(msg.Message.MsgType),
				Height:     qbft.Height(msg.Message.Height),
				Round:      qbft.Round(msg.Message.Round),
				Identifier: msg.Message.Identifier,
				Data:       msg.Message.Data,
			},
		})
		require.NoError(t, err)
		require.True(t, ok)
	})
	return c
}

func convertSingers(specSigners []types.OperatorID) []message.OperatorID {
	var signers []message.OperatorID
	for _, s := range specSigners {
		signers = append(signers, message.OperatorID(s))
	}
	return signers
}
