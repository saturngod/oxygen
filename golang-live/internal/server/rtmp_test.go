package server

import (
	"reflect"
	"testing"
)

func TestWithH264ParameterSetsPrependsOnKeyFrame(t *testing.T) {
	sps := []byte{0x67, 0x01}
	pps := []byte{0x68, 0x02}
	idr := []byte{0x65, 0x03}

	got := withH264ParameterSets([][]byte{idr}, sps, pps)
	want := [][]byte{sps, pps, idr}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWithH264ParameterSetsLeavesInBandAccessUnitAlone(t *testing.T) {
	sps := []byte{0x67, 0x01}
	pps := []byte{0x68, 0x02}
	au := [][]byte{sps, pps, {0x65, 0x03}}

	got := withH264ParameterSets(au, sps, pps)

	if !reflect.DeepEqual(got, au) {
		t.Fatalf("got %v, want %v", got, au)
	}
}

func TestWithH264ParameterSetsSkipsNonKeyFrames(t *testing.T) {
	au := [][]byte{{0x41, 0x03}} // non-IDR

	got := withH264ParameterSets(au, []byte{0x67, 0x01}, []byte{0x68, 0x02})

	if !reflect.DeepEqual(got, au) {
		t.Fatalf("got %v, want %v", got, au)
	}
}

func TestWithH264ParameterSetsSkipsWhenConfigMissing(t *testing.T) {
	au := [][]byte{{0x65, 0x03}}

	got := withH264ParameterSets(au, nil, nil)

	if !reflect.DeepEqual(got, au) {
		t.Fatalf("got %v, want %v", got, au)
	}
}

func TestWithH265ParameterSetsPrependsOnKeyFrame(t *testing.T) {
	vps := []byte{0x40, 0x01}
	sps := []byte{0x42, 0x01}
	pps := []byte{0x44, 0x01}
	idr := []byte{0x26, 0x01} // NALU type 19 (IDR_W_RADL)

	got := withH265ParameterSets([][]byte{idr}, vps, sps, pps)
	want := [][]byte{vps, sps, pps, idr}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWithH265ParameterSetsLeavesInBandAccessUnitAlone(t *testing.T) {
	vps := []byte{0x40, 0x01}
	sps := []byte{0x42, 0x01}
	pps := []byte{0x44, 0x01}
	au := [][]byte{vps, sps, pps, {0x26, 0x01}}

	got := withH265ParameterSets(au, vps, sps, pps)

	if !reflect.DeepEqual(got, au) {
		t.Fatalf("got %v, want %v", got, au)
	}
}
