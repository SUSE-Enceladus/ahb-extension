// Copyright (c) 2022, SUSE LLC, All rights reserved.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
// THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-extension-platform/pkg/exithelper"
	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/go-kit/kit/log"
)

const (
	extensionName                 = "AHBForSLES"
	extensionVersion              = "0.0.0.2"
	DEFAULT_SHELL_COMMAND_TIMEOUT = 120 //seconds
)

type AHBInfo struct {
	PublicCloudService     string
	RegisterCloudGuestPath string
	RegionSrvMinVer        string
	RegionSrvEnablerTimer  string
	RegionSrv              string
	RegionSrvAddOn         string
	RegionSrvPlugin        string
	RegionSrvConfig        string
	RegionSrvCerts         string
	AddonPath              string
	RepoAlias              string
	ModName                string
	RepoUrl                string
}

func parseCfg(filename string) (map[string]map[string]string, error) {
	ini := make(map[string]map[string]string)
	var head string

	fh, err := os.Open(filename)
	if err != nil {
		return ini, fmt.Errorf("Could not open file '%v': %v", filename, err)
	}
	sectionHead := regexp.MustCompile(`^\[([^]]*)\]\s*$`)
	keyValue := regexp.MustCompile(`^(\w*)\s*=\s*(.*?)\s*$`)
	reader := bufio.NewReader(fh)
	for {
		line, _ := reader.ReadString('\n')
		result := sectionHead.FindStringSubmatch(line)
		if len(result) > 0 {
			head = result[1]
			ini[head] = make(map[string]string)
			continue
		}

		result = keyValue.FindStringSubmatch(line)
		if len(result) > 0 {
			key, value := result[1], result[2]
			ini[head][key] = value
			continue
		}

		if line == "" {
			break
		}
	}

	return ini, nil
}

func _getSUSEConnectStatus() (bool, bool, error) {
	commandOutput, error := RunShellCommand(0, "SUSEConnect", "-s")
	if error != nil {
		fmt.Println(error)
		return false, false, error
	}
	var suseConnectStatus []map[string]interface{}
	json.Unmarshal([]byte(commandOutput), &suseConnectStatus)
	subscriptionStatus := fmt.Sprintf("%v", suseConnectStatus[0]["subscription_status"])
	status := fmt.Sprintf("%v", suseConnectStatus[0]["status"])
	if strings.ToLower(status) == "registered" && strings.ToLower(subscriptionStatus) == "active" {
		// system is registered with an active subscription
		// check if services are present
		services, err := filepath.Glob("/etc/zypp/services.d/*.service")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return true, true, err
		}
		if len(services) == 0 {
			extensionsOutput, err := RunShellCommand(0, "SUSEConnect", "--list-extensions")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return true, true, err
			}
			extensionsList := strings.Split(extensionsOutput, "\n")
			for _, extension := range extensionsList {
				if strings.Contains(extension, "Deactivate with") {
					// activate whatever it was active AND
					// Public Cloud module
					start := strings.Index(string(extension), "SUSEConnect")
					if start != -1 {
						command := string(extension)[start:len(extension)]
						commandList := strings.Split(command, " ")
						_, err = RunShellCommand(0, commandList[0], commandList[2], commandList[3])
						if err != nil {
							fmt.Fprintln(os.Stderr, err)
							return true, true, err
						}
					}
				}
			}
			return true, true, err
		}
		// check if repos are present
		repos, err := filepath.Glob("/etc/zypp/repos.d/*.repo")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return true, true, err
		}
		if len(repos) == 0 {
			_, err := RunShellCommand(0, "zypper", "refresh-services", "-f")
			return true, true, err
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return true, true, err
		}
		return true, true, err
	} else {
		if strings.ToLower(status) == "registered" {
			return true, false, nil
		}
	}
	return false, false, nil
}

func _hasPubCloudMod(pubCloudService string) (bool, string, string, error) {
	services, err := filepath.Glob("/etc/zypp/services.d/*.service")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return false, "", "", err
	}
	hasPubCloud := false
	distro := ""
	arch := ""

	for _, service := range services {
		serviceFile, _ := parseCfg(service)
		for _, value := range serviceFile {
			if strings.Contains(strings.ToLower(value["url"]), pubCloudService) {
				hasPubCloud = true
			} else {
				if distro == "" {
					if strings.Contains(value["repo_1"], "15-") {
						distro = "15"
					}
					if strings.Contains(value["repo_1"], "12-") {
						distro = "12"
					}
					if strings.Contains(value["repo_1"], "_x86_64") {
						arch = "x86_64"
					}
					if strings.Contains(value["repo_1"], "_aarch64") {
						arch = "aarch64"
					}
				}
				if distro != "" {
					if distro == "15" && strings.Contains(value["repo_1"], distro+"-SP") {
						// get SP if any
						indexSp := strings.LastIndex(value["repo_1"], distro+"-SP")
						sp := value["repo_1"][(indexSp + 5):(indexSp + 6)]
						distro = distro + "." + sp
					}
					break
				}
			}
		}
		if distro != "" {
			break
		}
	}
	return hasPubCloud, distro, arch, nil
}
func _isNewerVersion(versions []string) bool {
	installed := strings.Split(versions[0], ".")
	minVer := strings.Split(versions[1], ".")
	if len(installed) != len(minVer) || len(installed) != 3 {
		return versions[0] > versions[1]
	}

	installedInt := make([]int, len(installed))
	minVerInt := make([]int, len(minVer))
	for i := range installed {
		installedInt[i], _ = strconv.Atoi(installed[i])
		minVerInt[i], _ = strconv.Atoi(minVer[i])
	}

	// MAYOR.MINOR.PATCH
	// If MAJOR and MINOR are the same, compare PATCH
	if installedInt[0] == minVerInt[0] && installedInt[1] == minVerInt[1] {
		return installedInt[2] >= minVerInt[2]
	}

	// If MAJOR is same, compare MINOR
	if installedInt[0] == minVerInt[0] {
		return installedInt[1] >= minVerInt[1]
	}

	// Compare MAJOR
	return installedInt[0] >= minVerInt[0]
}

func _checkVersion(ahbInfo AHBInfo) bool {
	type Solvable struct {
		Name    string `xml:"name,attr"`
		Edition string `xml:"edition,attr"`
	}

	type ZypperOutput struct {
		XMLName   xml.Name   `xml:"stream"`
		Solvables []Solvable `xml:"search-result>solvable-list>solvable"`
	}

	var info ZypperOutput
	xmlData, err := RunShellCommand(0, "zypper", "-x", "search", "-is", ahbInfo.RegionSrv)
	err = xml.Unmarshal([]byte(xmlData), &info)
	if err != nil {
		fmt.Printf("error: %v", err)
		return false
	}
	for _, solvable := range info.Solvables {
		if solvable.Name == ahbInfo.RegionSrv {
			solvable.Edition = strings.Split(solvable.Edition, "-")[0]
			versionsToCompare := []string{solvable.Edition, ahbInfo.RegionSrvMinVer}
			if !_isNewerVersion(versionsToCompare) {
				return false
			} else {
				return true
			}
		}
	}
	return false
}

func _addRepo(repoAlias string, repoUrl string) error {
	_, err := RunShellCommand(0, "zypper", "addrepo", repoUrl, repoAlias)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error while adding a repo with URL:", repoUrl)
	}
	return err
}

func _installPackages(ahbInfo AHBInfo) error {
	regionSrv := fmt.Sprintf("%s>=%s", ahbInfo.RegionSrv, ahbInfo.RegionSrvMinVer)
	_, err := RunShellCommand(0, "zypper", "--non-interactive", "in", "--replacefiles",
		"--no-recommends", regionSrv, ahbInfo.RegionSrvAddOn, ahbInfo.RegionSrvPlugin,
		ahbInfo.RegionSrvConfig, ahbInfo.RegionSrvCerts)
	if err != nil {
		_, repoError := RunShellCommand(0, "zypper", "removerepo", ahbInfo.RepoAlias)
		if repoError != nil {
			fmt.Fprintln(os.Stderr, "Error when removing repo", ahbInfo.RepoAlias)
		}
		fmt.Fprintln(os.Stderr, "Error installing", ahbInfo.RegionSrv, "or", ahbInfo.RegionSrvAddOn)
		return err
	}
	return nil
}

func _getUnrestrictedRepoUrl(ahbRepoUrl string) string {
	output, _ := RunShellCommand(0, "uname", "-i")
	arch := strings.Trim(string(output), "\n\t\r")
	output, _ = RunShellCommand(0, "bash", "-c", "cat /etc/os-release | grep VERSION_ID")
	distro := ""
	if strings.Contains(string(output), "15") {
		distro = "15"
	}
	if strings.Contains(string(output), "12") {
		distro = "12"
	}
	return fmt.Sprintf(ahbRepoUrl, distro, arch)
}

func _installUnrestrictedRepoPackages(ahbInfo AHBInfo, repoUrl string) error {
	addRepoError := _addRepo(ahbInfo.RepoAlias, repoUrl)
	if addRepoError != nil {
		return addRepoError
	}
	// install cloud-regionsrv-client and addon packages
	if installError := _installPackages(ahbInfo); installError != nil {
		return installError
	}
	// packages installed, remove repo
	_, err := RunShellCommand(0, "zypper", "removerepo", ahbInfo.RepoAlias)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error when removing repo", ahbInfo.RepoAlias)
		return err
	}
	return nil
}

func _removeRepositories() error {
	repos, err := filepath.Glob("/etc/zypp/repos.d/*.repo")
	if err != nil {
		fmt.Println("Error getting repositories from '/etc/zypp/repos.d'")
		return err
	}
	for _, repo := range repos {
		repoFile, err := os.Open(repo)

		if err != nil {
			fmt.Println(err)
			return err
		}
		scanner := bufio.NewScanner(repoFile)

		for scanner.Scan() {
			fmt.Println(scanner.Text())
			if strings.Contains(scanner.Text(), "baseurl") && (strings.Contains(scanner.Text(), "plugin:/susecloud") ||
				strings.Contains(scanner.Text(), "plugin:susecloud")) {
				fmt.Println("Removing repo ", repo)
				os.Remove(repo)
			}
		}

		if err := scanner.Err(); err != nil {
			fmt.Println(err)
			return err
		}
	}
	return nil
}

func _handlePackageInstall(ahbInfo AHBInfo) error {
	isRegistered, hasActiveSubscription, err := _getSUSEConnectStatus()

	if err != nil {
		return err
	}

	if isRegistered && hasActiveSubscription {
		hasPubCloudMod, distro, arch, errGlob := _hasPubCloudMod(ahbInfo.PublicCloudService)
		if errGlob != nil {
			return errGlob
		}
		if !hasPubCloudMod {
			// add repo
			repoUrl := fmt.Sprintf(ahbInfo.RepoUrl, distro, arch)
			triplet := ahbInfo.ModName + "/" + distro + "/" + arch
			_, addRepoError := RunShellCommand(0, "SUSEConnect", "-p", triplet)
			if addRepoError != nil {
				// adding module with SUSEConnect failed,
				// trying adding repo with zypper
				addRepoError = _addRepo(ahbInfo.RepoAlias, repoUrl)
				if addRepoError != nil {
					return addRepoError
				}
			}
		}
		// install cloud-regionsrv-client and addon packages
		if installError := _installPackages(ahbInfo); installError != nil {
			return installError
		}
		// packages installed, remove repo
		_, err := RunShellCommand(0, "zypper", "removerepo", ahbInfo.RepoAlias)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error when removing repo", ahbInfo.RepoAlias)
		}
	} else {
		if isRegistered && !hasActiveSubscription {
			fmt.Println("System is registered but subscription expired. Removing repositories")
			_removeRepositories()
		}
		if !isRegistered {
			fmt.Println("System is not registered")
		}
		fmt.Println("Adding repository and installing packages")
		repoUrl := _getUnrestrictedRepoUrl(ahbInfo.RepoUrl)
		err := _installUnrestrictedRepoPackages(ahbInfo, repoUrl)
		if err != nil {
			fmt.Println("Error installing packages from Unrestricted repository")
			return err
		}
	}
	return nil
}

func getAhbInfo() AHBInfo {
	return AHBInfo{
		PublicCloudService:     "public_cloud",
		RegisterCloudGuestPath: "/usr/sbin/registercloudguest",
		RegionSrvMinVer:        "9.3.1",
		RegionSrvEnablerTimer:  "regionsrv-enabler-azure.timer",
		RegionSrv:              "cloud-regionsrv-client",
		RegionSrvAddOn:         "cloud-regionsrv-client-addon-azure",
		RegionSrvPlugin:        "cloud-regionsrv-client-plugin-azure",
		RegionSrvConfig:        "regionServiceClientConfigAzure",
		RegionSrvCerts:         "regionServiceCertsAzure",
		AddonPath:              "/usr/sbin/regionsrv-enabler-azure",
		RepoAlias:              "sle-ahb-packages",
		ModName:                "sle-module-public-cloud",
		RepoUrl:                "https://updates.suse.com/SUSE/Updates/SLE-Module-Public-Cloud-Unrestricted/%s/%s/update",
	}
}

var installCallbackFunc vmextension.CallbackFunc = func(ext *vmextension.VMExtension) error {
	ahbInfo := getAhbInfo()
	// 1. Check if the system has the public cloud module
	_, registercloudError := os.Stat(ahbInfo.RegisterCloudGuestPath)
	_, addonError := os.Stat(ahbInfo.AddonPath)
	if registercloudError == nil {
		if !_checkVersion(ahbInfo) {
			// need to install the right version
			handlePackageError := _handlePackageInstall(ahbInfo)
			if handlePackageError != nil {
				fmt.Fprintln(os.Stderr, "Extension install failed. Reason="+handlePackageError.Error())
				return handlePackageError
			}
		} else {
			if addonError == nil {
				// both packages are in the system and
				// the version is correct
				fmt.Println("Extension install succeeded")
				return nil
			} else {
				// missing addon package
				// add addon
				handlePackageError := _handlePackageInstall(ahbInfo)
				if handlePackageError != nil {
					fmt.Fprintln(os.Stderr, "Extension install failed. Reason="+handlePackageError.Error())
					return handlePackageError
				}
			}
		}
	} else {
		handlePackageError := _handlePackageInstall(ahbInfo)
		if handlePackageError != nil {
			fmt.Fprintln(os.Stderr, "Extension install failed. Reason="+handlePackageError.Error())
			return handlePackageError
		}
	}

	fmt.Println("Extension install succeeded")
	return nil
}

var enableCallbackFunc vmextension.EnableCallbackFunc = func(ext *vmextension.VMExtension) (string, error) {
	ahbInfo := getAhbInfo()
	//1. double check that the regionsrv-enabler-azure.service file exists
	status := "success"
	_, err := os.Stat(ahbInfo.AddonPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Extension enable failed. Reason="+err.Error())
		return "failure", err
	}
	//2. enable and start the timer
	systemdActions := []string{"enable", "start"}
	for _, systemdAction := range systemdActions {
		_, err = RunShellCommand(0, "systemctl", systemdAction, ahbInfo.RegionSrvEnablerTimer)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error when trying to", systemdAction, " timer", ahbInfo.RegionSrvEnablerTimer)
			status = "failure"
		}
	}
	fmt.Println(status, "when enabling the extension")
	return status, err
}

var updateCallbackFunc vmextension.CallbackFunc = func(ext *vmextension.VMExtension) error {
	// optional
	// on update, the extension will call this code
	return nil
}

var disableCallbackFunc vmextension.CallbackFunc = func(ext *vmextension.VMExtension) error {
	// optional
	// on disable, the extension will call this code
	return nil
}

var getVMExtensionFuncToCall = vmextension.GetVMExtension
var getInitializationInfoFuncToCall = vmextension.GetInitializationInfo

var logger = log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))

func main() {
	err := getExtensionAndRun()
	if err != nil {
		os.Exit(exithelper.EnvironmentError)
	}
}

func getExtensionAndRun() error {
	initilizationInfo, err := getInitializationInfoFuncToCall(extensionName, extensionVersion, false, enableCallbackFunc)
	if err != nil {
		return err
	}

	initilizationInfo.InstallCallback = installCallbackFunc
	initilizationInfo.DisableCallback = disableCallbackFunc
	initilizationInfo.UpdateCallback = updateCallbackFunc
	vmExt, err := getVMExtensionFuncToCall(initilizationInfo)
	if err != nil {
		return err
	}
	vmExt.Do()
	return nil
}

//Function to run a shell command through golang
func RunShellCommand(timeout time.Duration, name string, args ...string) (string, error) {

	if timeout == 0 {
		timeout = DEFAULT_SHELL_COMMAND_TIMEOUT
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		err = errors.New("Timeout running shell command: " + name + " " + strings.Join(args, " "))
		fmt.Fprintln(os.Stderr, err)
		return "", err
	}

	if err != nil {
		err = errors.New("Error running shell command: " + name + " " + strings.Join(args, " ") + ". Error: " + errb.String())
		fmt.Fprintln(os.Stderr, err)
		return "", err
	}

	return outb.String(), nil
}
