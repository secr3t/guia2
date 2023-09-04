package guia2

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/electricbubble/gadb"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var AdbServerHost = "localhost"
var AdbServerPort = gadb.AdbServerPort

var UIA2ServerPort = 6790

var DeviceTempPath = "/data/local/tmp"

type Device = gadb.Device

const forwardToPrefix = "forward-to-"

func DeviceList() (devices []Device, err error) {
	var adbClient gadb.Client
	if adbClient, err = gadb.NewClientWith(AdbServerHost, AdbServerPort); err != nil {
		return nil, err
	}

	return adbClient.DeviceList()
}

func NewUSBDriver(device ...Device) (driver *Driver, err error) {
	if len(device) == 0 {
		if device, err = DeviceList(); err != nil {
			return nil, err
		}
	}

	usbDevice := device[0]
	var localPort int
	if localPort, err = getFreePort(); err != nil {
		return nil, err
	}
	if err = usbDevice.Forward(localPort, UIA2ServerPort); err != nil {
		return nil, err
	}

	rawURL := fmt.Sprintf("http://%s%d:%d", forwardToPrefix, localPort, UIA2ServerPort)

	if driver, err = NewDriver(NewEmptyCapabilities(), rawURL); err != nil {
		_ = usbDevice.ForwardKill(localPort)
		return nil, err
	}
	driver.usbDevice = usbDevice
	driver.localPort = localPort
	return
}

func TerminateUIAutomator() (err error) {
	var devices []Device
	if devices, err = DeviceList(); err != nil {
		return err
	}
	usbDevice := devices[0]

	_, err = usbDevice.RunShellCommand("su", "-c", "pkill -f uiautomator")
	return
}

func LaunchUiAutomator2() (err error) {
	var devices []Device
	if devices, err = DeviceList(); err != nil {
		return err
	}
	usbDevice := devices[0]

	result, err := usbDevice.RunShellCommand("pgrep", "-f", "io.appium.uiautomator2.server.test")

	if result == "" {
		_, err = usbDevice.RunShellCommand("su", "-c", "pkill -f uiautomator")
		time.Sleep(time.Second)
		go usbDevice.RunShellCommand("am instrument", "-w", "-e", "disableAnalytics", "true", "io.appium.uiautomator2.server.test/androidx.test.runner.AndroidJUnitRunner")
	}

	return err
}

func NewWiFiDriver(ip string, uia2Port ...int) (driver *Driver, err error) {
	if len(uia2Port) == 0 {
		uia2Port = []int{UIA2ServerPort}
	}
	var devices []Device
	if devices, err = DeviceList(); err != nil {
		return nil, err
	}

	// rawURL := fmt.Sprintf("http://%s:%d", strings.Split(ip, ":")[0], uia2Port[0])
	rawURL := fmt.Sprintf("http://%s:%d", ip, uia2Port[0])

	var usbDevice gadb.Device
	for i := range devices {
		if strings.HasPrefix(devices[i].Serial(), ip) {
			dev := devices[i]
			deviceState, err := dev.State()
			if err != nil || deviceState != gadb.StateOnline {
				continue
			}
			usbDevice = dev
			break
		}
	}
	if usbDevice.Serial() == "" {
		return nil, errors.New("no matching and online device found")
	}
	if driver, err = NewDriver(NewEmptyCapabilities(), rawURL); err != nil {
		return nil, err
	}
	driver.usbDevice = usbDevice
	return
}

func (d *Driver) check() error {
	if d.usbDevice.Serial() == "" {
		return errors.New("adb daemon: the device is not ready")
	}
	return nil
}

// Dispose corresponds to the command:
//
//	adb -s $serial forward --remove $localPort
func (d *Driver) Dispose() (err error) {
	if err = d.check(); err != nil {
		return err
	}
	if d.localPort == 0 {
		return nil
	}
	return d.usbDevice.ForwardKill(d.localPort)
}

func (d *Driver) ActiveAppActivity() (appActivity string, err error) {
	if err = d.check(); err != nil {
		return "", err
	}

	var sOutput string
	if sOutput, err = d.RunShellCommand("dumpsys activity activities | grep mResumedActivity"); err != nil {
		return "", err
	}
	re := regexp.MustCompile(`\{(.+?)\}`)
	if !re.MatchString(sOutput) {
		return "", fmt.Errorf("active app activity: %s", strings.TrimSpace(sOutput))
	}
	fields := strings.Fields(re.FindStringSubmatch(sOutput)[1])
	appActivity = fields[2]
	return
}

func (d *Driver) ActiveAppPackageName() (appPackageName string, err error) {
	var activity string
	if activity, err = d.ActiveAppActivity(); err != nil {
		return "", err
	}
	appPackageName = strings.Split(activity, "/")[0]
	return
}

func (d *Driver) AppLaunch(appPackageName string, waitForComplete ...BySelector) (err error) {
	if err = d.check(); err != nil {
		return err
	}

	var sOutput string
	if sOutput, err = d.RunShellCommand("monkey -p", appPackageName, "-c android.intent.category.LAUNCHER 1"); err != nil {
		return err
	}
	if strings.Contains(sOutput, "monkey aborted") {
		return fmt.Errorf("app launch: %s", strings.TrimSpace(sOutput))
	}

	if len(waitForComplete) != 0 {
		var ce error
		exists := func(_d *Driver) (bool, error) {
			for i := range waitForComplete {
				_, ce = _d.FindElement(waitForComplete[i])
				if ce == nil {
					return true, nil
				}
			}
			return false, nil
		}
		if err = d.WaitWithTimeoutAndInterval(exists, 45, 1.5); err != nil {
			return fmt.Errorf("app launch (waitForComplete): %s: %w", err.Error(), ce)
		}
	}
	return
}

func (d *Driver) AppTerminate(appPackageName string) (err error) {
	if err = d.check(); err != nil {
		return err
	}

	_, err = d.RunShellCommand("am force-stop", appPackageName)
	return
}

func (d *Driver) AppInstall(apkPath string, reinstall ...bool) (err error) {
	if err = d.check(); err != nil {
		return err
	}

	apkName := filepath.Base(apkPath)
	if !strings.HasSuffix(strings.ToLower(apkName), ".apk") {
		return fmt.Errorf("apk file must have an extension of '.apk': %s", apkPath)
	}

	var apkFile *os.File
	if apkFile, err = os.Open(apkPath); err != nil {
		return fmt.Errorf("apk file: %w", err)
	}

	remotePath := path.Join(DeviceTempPath, apkName)
	if err = d.usbDevice.PushFile(apkFile, remotePath); err != nil {
		return fmt.Errorf("apk push: %w", err)
	}

	var shellOutput string
	if len(reinstall) != 0 && reinstall[0] {
		shellOutput, err = d.RunShellCommand("pm install", "-r", remotePath)
	} else {
		shellOutput, err = d.RunShellCommand("pm install", remotePath)
	}

	if err != nil {
		return fmt.Errorf("apk install: %w", err)
	}

	if !strings.Contains(shellOutput, "Success") {
		return fmt.Errorf("apk installed: %s", shellOutput)
	}

	return
}

func (d *Driver) AppUninstall(appPackageName string, keepDataAndCache ...bool) (err error) {
	if err = d.check(); err != nil {
		return err
	}

	var shellOutput string
	if len(keepDataAndCache) != 0 && keepDataAndCache[0] {
		shellOutput, err = d.RunShellCommand("pm uninstall", "-k", appPackageName)
	} else {
		shellOutput, err = d.RunShellCommand("pm uninstall", appPackageName)
	}

	if err != nil {
		return fmt.Errorf("apk uninstall: %w", err)
	}

	if !strings.Contains(shellOutput, "Success") {
		return fmt.Errorf("apk uninstalled: %s", shellOutput)
	}

	return
}

func (d *Driver) RunShellCommand(cmd string, args ...string) (string, error) {
	return d.usbDevice.RunShellCommand(cmd, args...)
}

func (d *Driver) GetDevice() gadb.Device {
	return d.usbDevice
}

func (d *Driver) Push(buf []byte, remotePath string, mode ...os.FileMode) error {
	return d.usbDevice.Push(bytes.NewBuffer(buf), remotePath, time.Now(), mode...)
}

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("free port: %w", err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, fmt.Errorf("free port: %w", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

type UiSelectorHelper struct {
	value *bytes.Buffer
}

func NewUiSelectorHelper() UiSelectorHelper {
	return UiSelectorHelper{value: bytes.NewBufferString("new UiSelector()")}
}

func (s UiSelectorHelper) String() string {
	return s.value.String() + ";"
}

// Text Set the search criteria to match the visible text displayed
// in a widget (for example, the text label to launch an app).
//
// The text for the element must match exactly with the string in your input
// argument. Matching is case-sensitive.
func (s UiSelectorHelper) Text(text string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.text("%s")`, text))
	return s
}

// TextMatches Set the search criteria to match the visible text displayed in a layout
// element, using a regular expression.
//
// The text in the widget must match exactly with the string in your
// input argument.
func (s UiSelectorHelper) TextMatches(regex string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.textMatches("%s")`, regex))
	return s
}

// TextStartsWith Set the search criteria to match visible text in a widget that is
// prefixed by the text parameter.
//
// The matching is case-insensitive.
func (s UiSelectorHelper) TextStartsWith(text string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.textStartsWith("%s")`, text))
	return s
}

// TextContains Set the search criteria to match the visible text in a widget
// where the visible text must contain the string in your input argument.
//
// The matching is case-sensitive.
func (s UiSelectorHelper) TextContains(text string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.textContains("%s")`, text))
	return s
}

// ClassName Set the search criteria to match the class property
// for a widget (for example, "android.widget.Button").
func (s UiSelectorHelper) ClassName(className string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.className("%s")`, className))
	return s
}

// ClassNameMatches Set the search criteria to match the class property
// for a widget, using a regular expression.
func (s UiSelectorHelper) ClassNameMatches(regex string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.classNameMatches("%s")`, regex))
	return s
}

// Description Set the search criteria to match the content-description
// property for a widget.
//
// The content-description is typically used
// by the Android Accessibility framework to
// provide an audio prompt for the widget when
// the widget is selected. The content-description
// for the widget must match exactly
// with the string in your input argument.
//
// Matching is case-sensitive.
func (s UiSelectorHelper) Description(desc string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.description("%s")`, desc))
	return s
}

// DescriptionMatches Set the search criteria to match the content-description
// property for a widget.
//
// The content-description is typically used
// by the Android Accessibility framework to
// provide an audio prompt for the widget when
// the widget is selected. The content-description
// for the widget must match exactly
// with the string in your input argument.
func (s UiSelectorHelper) DescriptionMatches(regex string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.descriptionMatches("%s")`, regex))
	return s
}

// DescriptionStartsWith Set the search criteria to match the content-description
// property for a widget.
//
// The content-description is typically used
// by the Android Accessibility framework to
// provide an audio prompt for the widget when
// the widget is selected. The content-description
// for the widget must start
// with the string in your input argument.
//
// Matching is case-insensitive.
func (s UiSelectorHelper) DescriptionStartsWith(desc string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.descriptionStartsWith("%s")`, desc))
	return s
}

// DescriptionContains Set the search criteria to match the content-description
// property for a widget.
//
// The content-description is typically used
// by the Android Accessibility framework to
// provide an audio prompt for the widget when
// the widget is selected. The content-description
// for the widget must contain
// the string in your input argument.
//
// Matching is case-insensitive.
func (s UiSelectorHelper) DescriptionContains(desc string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.descriptionContains("%s")`, desc))
	return s
}

// ResourceId Set the search criteria to match the given resource ID.
func (s UiSelectorHelper) ResourceId(id string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.resourceId("%s")`, id))
	return s
}

// ResourceIdMatches Set the search criteria to match the resource ID
// of the widget, using a regular expression.
func (s UiSelectorHelper) ResourceIdMatches(regex string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.resourceIdMatches("%s")`, regex))
	return s
}

// Index Set the search criteria to match the widget by its node
// index in the layout hierarchy.
//
// The index value must be 0 or greater.
//
// Using the index can be unreliable and should only
// be used as a last resort for matching. Instead,
// consider using the `Instance(int)` method.
func (s UiSelectorHelper) Index(index int) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.index(%d)`, index))
	return s
}

// Instance Set the search criteria to match the
// widget by its instance number.
//
// The instance value must be 0 or greater, where
// the first instance is 0.
//
// For example, to simulate a user click on
// the third image that is enabled in a UI screen, you
// could specify a a search criteria where the instance is
// 2, the `className(String)` matches the image
// widget class, and `enabled(boolean)` is true.
// The code would look like this:
//
//	`new UiSelector().className("android.widget.ImageView")
//	  .enabled(true).instance(2);`
func (s UiSelectorHelper) Instance(instance int) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.instance(%d)`, instance))
	return s
}

// Enabled Set the search criteria to match widgets that are enabled.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Enabled(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.enabled(%t)`, b))
	return s
}

// Focused Set the search criteria to match widgets that have focus.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Focused(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.focused(%t)`, b))
	return s
}

// Focusable Set the search criteria to match widgets that are focusable.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Focusable(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.focusable(%t)`, b))
	return s
}

// Scrollable Set the search criteria to match widgets that are scrollable.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Scrollable(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.scrollable(%t)`, b))
	return s
}

// Selected Set the search criteria to match widgets that
// are currently selected.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Selected(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.selected(%t)`, b))
	return s
}

// Checked Set the search criteria to match widgets that
// are currently checked (usually for checkboxes).
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Checked(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.checked(%t)`, b))
	return s
}

// Checkable Set the search criteria to match widgets that are checkable.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Checkable(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.checkable(%t)`, b))
	return s
}

// Clickable Set the search criteria to match widgets that are clickable.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Clickable(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.clickable(%t)`, b))
	return s
}

// LongClickable Set the search criteria to match widgets that are long-clickable.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) LongClickable(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.longClickable(%t)`, b))
	return s
}

// packageName Set the search criteria to match the package name
// of the application that contains the widget.
func (s UiSelectorHelper) packageName(name string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.packageName(%s)`, name))
	return s
}

// PackageNameMatches Set the search criteria to match the package name
// of the application that contains the widget.
func (s UiSelectorHelper) PackageNameMatches(regex string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.packageNameMatches(%s)`, regex))
	return s
}

// ChildSelector Adds a child UiSelector criteria to this selector.
//
// Use this selector to narrow the search scope to
// child widgets under a specific parent widget.
func (s UiSelectorHelper) ChildSelector(selector UiSelectorHelper) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.childSelector(%s)`, selector.value.String()))
	return s
}

func (s UiSelectorHelper) PatternSelector(selector UiSelectorHelper) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.patternSelector(%s)`, selector.value.String()))
	return s
}

func (s UiSelectorHelper) ContainerSelector(selector UiSelectorHelper) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.containerSelector(%s)`, selector.value.String()))
	return s
}

// FromParent Adds a child UiSelector criteria to this selector which is used to
// start search from the parent widget.
//
// Use this selector to narrow the search scope to
// sibling widgets as well all child widgets under a parent.
func (s UiSelectorHelper) FromParent(selector UiSelectorHelper) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.fromParent(%s)`, selector.value.String()))
	return s
}
