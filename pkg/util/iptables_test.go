package util

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/mkimuram/k8s-ext-connector/pkg/util/mock_util"
)

func TestDNATRuleSpec(t *testing.T) {
	testCases := []struct {
		name            string
		dstIP           string
		srcIP           string
		dPort           string
		destinationIP   string
		destinationPort string
		spec            []string
		expected        bool
	}{
		{
			name:            "Normal case (should return the same result)",
			dstIP:           "192.168.122.201",
			srcIP:           "192.168.122.140",
			dPort:           "80",
			destinationIP:   "192.168.122.200",
			destinationPort: "2049",
			spec:            []string{"-m", "tcp", "-p", "tcp", "--dst", "192.168.122.201", "--src", "192.168.122.140", "--dport", "80", "-j", "DNAT", "--to-destination", "192.168.122.200:2049"},
			expected:        true,
		},
		{
			name: "Error case (should return the different result)",
			// dstIP is different
			dstIP:           "192.168.122.202",
			srcIP:           "192.168.122.140",
			dPort:           "80",
			destinationIP:   "192.168.122.200",
			destinationPort: "2049",
			//  spec is the same to above
			spec:     []string{"-m", "tcp", "-p", "tcp", "--dst", "192.168.122.201", "--src", "192.168.122.140", "--dport", "80", "-j", "DNAT", "--to-destination", "192.168.122.200:2049"},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		spec := DNATRuleSpec(tc.dstIP, tc.srcIP, tc.dPort, tc.destinationIP, tc.destinationPort)
		if reflect.DeepEqual(spec, tc.spec) != tc.expected {
			if tc.expected {
				t.Errorf("expecting spec %s, but got %s", tc.spec, spec)
			} else {
				t.Errorf("not expecting spec %s, and got %s", tc.spec, spec)
			}
		}

	}
}

func TestSNATRuleSpec(t *testing.T) {
	testCases := []struct {
		name     string
		dstIP    string
		srcIP    string
		dPort    string
		spec     []string
		expected bool
	}{
		{
			name:     "Normal case (should return the same result)",
			dstIP:    "192.168.122.201",
			srcIP:    "192.168.122.140",
			dPort:    "80",
			spec:     []string{"-m", "tcp", "-p", "tcp", "--dst", "192.168.122.201", "--dport", "80", "-j", "SNAT", "--to-source", "192.168.122.140"},
			expected: true,
		},
		{
			name: "Error case (should return the different result)",
			// dstIP is different
			dstIP: "192.168.122.202",
			srcIP: "192.168.122.140",
			dPort: "80",
			//  spec is the same to above
			spec:     []string{"-m", "tcp", "-p", "tcp", "--dst", "192.168.122.201", "--dport", "80", "-j", "SNAT", "--to-source", "192.168.122.140"},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		spec := SNATRuleSpec(tc.dstIP, tc.srcIP, tc.dPort)
		if reflect.DeepEqual(spec, tc.spec) != tc.expected {
			if tc.expected {
				t.Errorf("expecting spec %s, but got %s", tc.spec, spec)
			} else {
				t.Errorf("not expecting spec %s, and got %s", tc.spec, spec)
			}
		}
	}
}

var (
	preRoutingRule1  = []string{"-m", "tcp", "-p", "tcp", "--dst", "192.168.122.201", "--src", "192.16    8.122.140", "--dport", "80", "-j", "DNAT", "--to-destination", "192.168.122.200:2049"}
	preRoutingRule2  = []string{"-m", "tcp", "-p", "tcp", "--dst", "192.168.122.202", "--src", "192.16    8.122.140", "--dport", "80", "-j", "DNAT", "--to-destination", "192.168.122.200:2049"}
	postRoutingRule1 = []string{"-m", "tcp", "-p", "tcp", "--dst", "192.168.122.201", "--dport", "80", "-j", "SNAT", "--to-source", "192.168.122.140"}
	postRoutingRule2 = []string{"-m", "tcp", "-p", "tcp", "--dst", "192.168.122.202", "--dport", "80", "-j", "SNAT", "--to-source", "192.168.122.140"}
)

func toInterfaceSlice(s []string) []interface{} {
	ret := make([]interface{}, len(s))
	for i := range s {
		ret[i] = s[i]
	}
	return ret
}

func TestAppendChains(t *testing.T) {
	type clearChainCall struct {
		table string
		chain string
		ret   error
	}
	type appendUniqueCall struct {
		table    string
		chain    string
		ruleSpec []string
		ret      error
	}

	testCases := []struct {
		name                     string
		table                    string
		rules                    map[string][][]string
		flash                    bool
		fail                     bool
		expectedClearChainCall   []clearChainCall
		expectedAppendUniqueCall []appendUniqueCall
	}{
		{
			name:  "Normal case (Add two rules to two chains in nat table with flash flag on)",
			table: "nat",
			rules: map[string][][]string{
				"PREROUTING": [][]string{
					preRoutingRule1,
					preRoutingRule2,
				},
				"POSTROUTING": [][]string{
					postRoutingRule1,
					postRoutingRule2,
				},
			},
			flash: true,
			fail:  false,
			expectedClearChainCall: []clearChainCall{
				{table: "nat", chain: "PREROUTING", ret: nil},
				{table: "nat", chain: "POSTROUTING", ret: nil},
			},
			expectedAppendUniqueCall: []appendUniqueCall{
				{table: "nat", chain: "PREROUTING", ruleSpec: preRoutingRule1, ret: nil},
				{table: "nat", chain: "PREROUTING", ruleSpec: preRoutingRule2, ret: nil},
				{table: "nat", chain: "POSTROUTING", ruleSpec: postRoutingRule1, ret: nil},
				{table: "nat", chain: "POSTROUTING", ruleSpec: postRoutingRule2, ret: nil},
			},
		},
		{
			name:  "Normal case (Add two rules to two chains in nat table with flash flag off)",
			table: "nat",
			rules: map[string][][]string{
				"PREROUTING": [][]string{
					preRoutingRule1,
					preRoutingRule2,
				},
				"POSTROUTING": [][]string{
					postRoutingRule1,
					postRoutingRule2,
				},
			},
			// flash flag off
			flash: false,
			fail:  false,
			// ClearChain should not be called
			expectedClearChainCall: []clearChainCall{},
			expectedAppendUniqueCall: []appendUniqueCall{
				{table: "nat", chain: "PREROUTING", ruleSpec: preRoutingRule1, ret: nil},
				{table: "nat", chain: "PREROUTING", ruleSpec: preRoutingRule2, ret: nil},
				{table: "nat", chain: "POSTROUTING", ruleSpec: postRoutingRule1, ret: nil},
				{table: "nat", chain: "POSTROUTING", ruleSpec: postRoutingRule2, ret: nil},
			},
		},
		{
			name:  "Error case (Fail in ClearChain and return error)",
			table: "nat",
			rules: map[string][][]string{
				"PREROUTING": [][]string{
					preRoutingRule1,
				},
			},
			flash: true,
			// Should fail due to error in ClearChain
			fail: true,
			expectedClearChainCall: []clearChainCall{
				// Return error
				{table: "nat", chain: "PREROUTING", ret: fmt.Errorf("failed to clear POSTROUTING chain")},
			},
			expectedAppendUniqueCall: []appendUniqueCall{},
		},
		{
			name:  "Error case (Fail in AppendUnique and return error)",
			table: "nat",
			rules: map[string][][]string{
				"PREROUTING": [][]string{
					preRoutingRule1,
				},
			},
			// flash flag off
			flash: false,
			// Should fail due to error in AppendUnique
			fail: true,
			// ClearChain should not be called
			expectedClearChainCall: []clearChainCall{},
			expectedAppendUniqueCall: []appendUniqueCall{
				{table: "nat", chain: "PREROUTING", ruleSpec: preRoutingRule1, ret: fmt.Errorf("failed to append PREROUTING chain")},
			},
		},
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		mipt := mock_util.NewMockIptables(ctrl)
		for _, c := range tc.expectedClearChainCall {
			mipt.EXPECT().ClearChain(c.table, c.chain).Return(c.ret)
		}
		for _, c := range tc.expectedAppendUniqueCall {
			spec := toInterfaceSlice(c.ruleSpec)
			mipt.EXPECT().AppendUnique(c.table, c.chain, spec...).Return(c.ret)
		}

		err := appendChains(mipt, tc.table, tc.rules, tc.flash)
		if err == nil && tc.fail {
			t.Errorf("expected to return error, but error was not returned")
		}
		if err != nil && !tc.fail {
			t.Errorf("expected no error but error was returned: %v", err)
		}
	}
}

func TestCheckChainsExist(t *testing.T) {
	type existsCall struct {
		table    string
		chain    string
		ruleSpec []string
		exist    bool
		err      error
	}

	testCases := []struct {
		name               string
		table              string
		rules              map[string][][]string
		exist              bool
		expectedExistsCall []existsCall
	}{
		{
			name:  "Normal case (Check two rules in two chains in nat table and return true)",
			table: "nat",
			rules: map[string][][]string{
				"PREROUTING": [][]string{
					preRoutingRule1,
					preRoutingRule2,
				},
				"POSTROUTING": [][]string{
					postRoutingRule1,
					postRoutingRule2,
				},
			},
			exist: true,
			expectedExistsCall: []existsCall{
				{table: "nat", chain: "PREROUTING", ruleSpec: preRoutingRule1, exist: true, err: nil},
				{table: "nat", chain: "PREROUTING", ruleSpec: preRoutingRule2, exist: true, err: nil},
				{table: "nat", chain: "POSTROUTING", ruleSpec: postRoutingRule1, exist: true, err: nil},
				{table: "nat", chain: "POSTROUTING", ruleSpec: postRoutingRule2, exist: true, err: nil},
			},
		},
		{
			name:  "Normal case (return false in Exists call)",
			table: "nat",
			rules: map[string][][]string{
				"PREROUTING": [][]string{
					preRoutingRule1,
				},
			},
			// should return false
			exist: false,
			expectedExistsCall: []existsCall{
				// return false
				{table: "nat", chain: "PREROUTING", ruleSpec: preRoutingRule1, exist: false, err: nil},
			},
		},
		{
			name:  "Error case (return error in Exists call)",
			table: "nat",
			rules: map[string][][]string{
				"PREROUTING": [][]string{
					preRoutingRule1,
				},
			},
			// should return false
			exist: false,
			expectedExistsCall: []existsCall{
				// return error
				{table: "nat", chain: "PREROUTING", ruleSpec: preRoutingRule1, exist: true, err: fmt.Errorf("error in exists call")},
			},
		},
	}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		mipt := mock_util.NewMockIptables(ctrl)
		for _, c := range tc.expectedExistsCall {
			spec := toInterfaceSlice(c.ruleSpec)
			mipt.EXPECT().Exists(c.table, c.chain, spec...).Return(c.exist, c.err)
		}

		exist := checkChainsExist(mipt, tc.table, tc.rules)
		if tc.exist != exist {
			t.Errorf("expected %v, but got %v", tc.exist, exist)
		}
	}
}
