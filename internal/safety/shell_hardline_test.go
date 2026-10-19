package safety

import "testing"

func TestCheckShellCommand_HardlineGaps(t *testing.T) {
	blocked := []string{
		"rm -rf -- /*",
		"rm -rf /\necho done",
		"rm -rf /*\nls",
		"rm -rf / # cleanup",
		"rm -r -f -- /",
		"rm -rf --one-file-system /",
		"rm -rf / /tmp/x",
		"sudo rm -rf -- / ;",
		"(rm -rf /)",
		"dd if=/dev/zero of=/dev/rdisk2",
		"dd if=x of=/dev/mmcblk0",
		"dd if=x of=/dev/vda",
		"dd if=x of=/dev/xvda bs=1m",
		"diskutil secureErase 0 disk2",
		"diskutil zeroDisk disk2",
		"diskutil randomDisk 1 disk2",
		"diskutil partitionDisk disk2 GPT JHFS+ x 100%",
		"sudo diskutil reformat disk3s2",
	}
	for _, c := range blocked {
		if err := CheckShellCommand(c); err == nil {
			t.Errorf("CheckShellCommand(%q) = nil, want refusal", c)
		}
	}
	allowed := []string{
		"rm -rf /tmp/build",
		"rm -rf ./x",
		"rm -rf /*.log",
		"rm -rf /var/cache/foo",
		"rm -rf build/",
		"rm -f /",
		"dd if=/dev/zero of=/tmp/file bs=1m count=1",
		"dd if=x of=/dev/null",
		"diskutil list",
		"diskutil info disk2",
		"echo diskutil zeroDisk",
	}
	for _, c := range allowed {
		if err := CheckShellCommand(c); err != nil {
			t.Errorf("CheckShellCommand(%q) = %v, want nil", c, err)
		}
	}
}
