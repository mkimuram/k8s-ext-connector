package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/golang/glog"
	"github.com/spf13/viper"
)

var (
	confPath = flag.String("conf", "/etc/external-service/config/data.yaml", "Path for the config file.")
)

func init() {
	flag.Parse()
}

func registerConfigHandler(path string) error {
	v := viper.New()
	v.AddConfigPath(filepath.Dir(path))
	v.SetConfigName(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))

	// Do first call of action
	if err := action(v); err != nil {
		return err
	}

	// Register action on change
	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		action(v)
	})

	return nil
}

func action(v *viper.Viper) error {
	if err := v.ReadInConfig(); err != nil {
		glog.Errorf("failed to read config: %v", err)
		return err
	}

	if err := updateSSHTunnel(getExpected(v, "ssh-tunnel")); err != nil {
		glog.Errorf("failed to update ssh tunnel: %v", err)
		return err
	}

	if err := updateRemoteSSHTunnel(getExpected(v, "remote-ssh-tunnel")); err != nil {
		glog.Errorf("failed to update remote ssh tunnel: %v", err)
		return err
	}

	if err := updateIptablesRule(getExpected(v, "iptables-rule")); err != nil {
		glog.Errorf("failed to update iptables rule: %v", err)
		return err
	}

	return nil
}

func getExpected(v *viper.Viper, key string) []string {
	val := v.Get(key)
	if str, ok := val.(string); ok {
		return strings.Split(str, "\n")
	}
	return []string{}
}

func getExistingTunnel(options string) (map[string]string, error) {
	ret := map[string]string{}
	// Get sshd process
	// Expected output format is:
	//   {pid} {args}...
	// ex)
	//   2149231 ssh -o StrictHostKeyChecking=no -i /etc/ssh-key/id_rsa -f -N -R 192.168.122.201:2049:10.96.218.78:80 192.168.122.201
	//   2747420 ssh -o StrictHostKeyChecking=no -i /etc/ssh-key/id_rsa -g -f -N -L 2049:192.168.122.140:8000 192.168.122.201
	cmd := exec.Command("ps", "-C", "ssh", "-o", "pid,args", "--no-headers")
	out, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			// ps command will return exit code 1 if no process found,
			// so return error only when it returns other than 1
			if exitError.ExitCode() != 1 {
				return ret, fmt.Errorf("failed to get ssh process: %v", err)
			}
		}
	}

	// Get only pids that has {options} in arguments, and put them to map
	// {options} will be "-g -f -N -L" or "-f -N -R"
	for _, s := range strings.Split(string(out), "\n") {
		if !strings.Contains(s, options) {
			// Skip unmatched line
			continue
		}
		fields := strings.Fields(s)
		// fields needs to be longer than 2, to access to fields[len(fields)-2] below
		if len(fields) < 2 {
			glog.Warningf("invalid process string %q: %v", s, err)
			continue
		}
		// pid should be "2747420" in above case, if {options} is "-g -f -N -L"
		pid := fields[0]
		// args should be "2049:192.168.122.140:8000 192.168.122.201" in above case
		// if {options} is "-g -f -N -L"
		args := fmt.Sprintf("%s %s", fields[len(fields)-2], fields[len(fields)-1])
		ret[args] = pid
	}

	return ret, nil
}

func deleteUnusedTunnel(expected []string, options string) error {
	expectedMap := map[string]bool{}
	for _, val := range expected {
		expectedMap[val] = true
	}

	existing, err := getExistingTunnel(options)
	if err != nil {
		return err
	}

	for tun, pid := range existing {
		if _, ok := expectedMap[tun]; ok {
			// Existing tunnel is expected, no need to delete this tunnel
			continue
		}
		// delete unused tunnel
		cmd := exec.Command("kill", pid)
		if err := cmd.Run(); err != nil {
			// TODO: handle error properly
			glog.Errorf("failed to execute command %v: %v", cmd, err)
		}
	}

	return nil
}

func ensureTunnel(expected []string, options string) error {
	existing, err := getExistingTunnel(options)
	if err != nil {
		return err
	}

	for _, tun := range expected {
		if _, ok := existing[tun]; ok {
			// Already exists, so skip creating tunnel
			continue
		}

		args := []string{"-o", "StrictHostKeyChecking=no", "-i", "/etc/ssh-key/id_rsa"}
		optionStrs := strings.Fields(options)
		args = append(args, optionStrs...)
		tunStrs := strings.Fields(tun)
		if len(tunStrs) == 0 {
			// Skip empty rule
			continue
		}
		args = append(args, tunStrs...)
		cmd := exec.Command("ssh", args...)
		if err := cmd.Start(); err != nil {
			// TODO: handle error properly
			glog.Errorf("failed to execute command %v: %v", cmd, err)
		}
	}

	return nil
}

func updateTunnel(expected []string, options string) error {
	if err := deleteUnusedTunnel(expected, options); err != nil {
		return err
	}

	return ensureTunnel(expected, options)
}

func updateSSHTunnel(expected []string) error {
	return updateTunnel(expected, "-g -f -N -L" /* options */)
}

func updateRemoteSSHTunnel(expected []string) error {
	return updateTunnel(expected, "-f -N -R" /* options */)
}

func updateIptablesRule(expected []string) error {
	// Flash existing nat rules
	cmd := exec.Command("iptables", "-t", "nat", "-F")
	if err := cmd.Run(); err != nil {
		glog.Errorf("failed to execute command %v: %v", cmd, err)
		return err
	}

	// Apply all rules
	for _, rule := range expected {
		args := []string{"-A"}
		ruleStrs := strings.Fields(rule)
		if len(ruleStrs) == 0 {
			// Skip empty rule
			continue
		}
		args = append(args, ruleStrs...)
		cmd := exec.Command("iptables", args...)

		if err := cmd.Run(); err != nil {
			// TODO: handle error properly
			glog.Errorf("failed to execute command %v: %v", cmd, err)
		}
	}

	return nil
}

func main() {
	if err := registerConfigHandler(*confPath); err != nil {
		fmt.Println("error:", err.Error())
		os.Exit(1)
	}

	// Wait forever
	select {}
}
