// Copyright (c) 2022, SUSE LLC, All rights reserved.
//
// This library is free software; you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public
// License as published by the Free Software Foundation; either
// version 2.0 of the License, or (at your option) any later version.
// This library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
// Lesser General Public License for more details.
// You should have received a copy of the GNU Lesser General Public
// License along with this library.

package main

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Azure/azure-extension-platform/pkg/exithelper"
	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/go-kit/kit/log"
)

const (
	extensionName    = "AHBForSLES"
	extensionVersion = "0.0.0.1"
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

func _isRegistered() (bool, error) {
	services, err := filepath.Glob("/etc/zypp/services.d/*.service")
	if err != nil {
		fmt.Println(err)
		return false, err
	}
	return len(services) > 0, err
}

func _hasPubCloudMod(pubCloudService string) (bool, string, string, error) {
	services, err := filepath.Glob("/etc/zypp/services.d/*.service")
	if err != nil {
		fmt.Println(err)
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
					if strings.Contains(value["repo_1"], distro+"-SP") {
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
	xmlData, err := exec.Command("zypper", "-x", "search", "-is", ahbInfo.RegionSrv).Output()
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
	_, err := exec.Command("zypper", "addrepo", repoUrl, repoAlias).Output()
	if err != nil {
		fmt.Println("Error while adding a repo with URL:", repoUrl)
	}
	return err
}

func _installPackages(ahbInfo AHBInfo) error {
	regionSrv := fmt.Sprintf("%s>=%s", ahbInfo.RegionSrv, ahbInfo.RegionSrvMinVer)
	_, err := exec.Command("zypper", "--non-interactive", "in", "--replacefiles",
		"--no-recommends", regionSrv, ahbInfo.RegionSrvAddOn, ahbInfo.RegionSrvPlugin,
		ahbInfo.RegionSrvConfig, ahbInfo.RegionSrvCerts).Output()
	if err != nil {
		_, repoError := exec.Command("zypper", "removerepo", ahbInfo.RepoAlias).Output()
		if repoError != nil {
			fmt.Println("Error when removing repo", ahbInfo.RepoAlias)
		}
		fmt.Println("Error installing", ahbInfo.RegionSrv, "or", ahbInfo.RegionSrvAddOn)
		return err
	}
	return nil
}

func _handlePackageInstall(ahbInfo AHBInfo) error {
	isRegistered, err := _isRegistered()

	if err != nil {
		return err
	}

	if isRegistered {
		hasPubCloudMod, distro, arch, errGlob := _hasPubCloudMod(ahbInfo.PublicCloudService)
		if errGlob != nil {
			return errGlob
		}
		if !hasPubCloudMod {
			// add repo
			repoUrl := fmt.Sprintf(ahbInfo.RepoUrl, distro, arch)
			triplet := ahbInfo.ModName + "/" + distro + "/" + arch
			_, addRepoError := exec.Command("SUSEConnect", "-p", triplet).Output()
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
		_, err := exec.Command("zypper", "removerepo", ahbInfo.RepoAlias).Output()
		if err != nil {
			fmt.Println("Error when removing repo", ahbInfo.RepoAlias)
		}
	} else {
		fmt.Println(
			"System is not registered. Adding repository and installing packages.",
		)
		output, _ := exec.Command("uname", "-i").Output()
		arch := strings.Trim(string(output), "\n\t\r")
		output, _ = exec.Command("cat", "/etc/os-release", "|", "grep", "VERSION_ID").Output()
		distro := ""
		if strings.Contains(string(output), "15") {
			distro = "15"
		}
		if strings.Contains(string(output), "12") {
			distro = "12"
		}
		repoUrl := fmt.Sprintf(ahbInfo.RepoUrl, distro, arch)
		addRepoError := _addRepo(ahbInfo.RepoAlias, repoUrl)
		if addRepoError != nil {
			return addRepoError
		}
		// install cloud-regionsrv-client and addon packages
		if installError := _installPackages(ahbInfo); installError != nil {
			return installError
		}
		// packages installed, remove repo
		_, err := exec.Command("zypper", "removerepo", ahbInfo.RepoAlias).Output()
		if err != nil {
			fmt.Println("Error when removing repo", ahbInfo.RepoAlias)
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
				return handlePackageError
			}
		} else {
			if addonError == nil {
				// both packages are in the system and
				// the version is correct
				return nil
			} else {
				// missing addon package
				// add addon
				handlePackageError := _handlePackageInstall(ahbInfo)
				if handlePackageError != nil {
					return handlePackageError
				}
			}
		}
	} else {
		handlePackageError := _handlePackageInstall(ahbInfo)
		if handlePackageError != nil {
			return handlePackageError
		}
	}
	return nil
}

var enableCallbackFunc vmextension.EnableCallbackFunc = func(ext *vmextension.VMExtension) (string, error) {
	ahbInfo := getAhbInfo()
	//1. double check that the regionsrv-enabler-azure.service file exists
	status := "success"
	_, err := os.Stat(ahbInfo.AddonPath)
	if err != nil {
		return "failure", err
	}
	//2. enable the timer
	_, err = exec.Command("systemctl", "enable", ahbInfo.RegionSrvEnablerTimer).Output()
	if err != nil {
		fmt.Println("Error when enabling timer", ahbInfo.RegionSrvEnablerTimer)
		status = "failure"
	}

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
	initilizationInfo, err := getInitializationInfoFuncToCall(extensionName, extensionVersion, true, enableCallbackFunc)
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
