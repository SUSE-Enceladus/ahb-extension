// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.
// Sample code to for how to use azure-extension-helper with your extension

package main

import (
	"os"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/go-kit/kit/log"
)

const (
	extensionName    = "TestExtension"
	extensionVersion = "0.0.0.1"
)

var enableCallbackFunc vmextension.EnableCallbackFunc = func(ext *vmextension.VMExtension) (string, error) {
	// put your extension specific code here
	// on enable, the extension will call this code
  s := SLES{
    PublicCloudService: "public_cloud",
    RegisterCloudGuestPath: "/usr/sbin/registercloudguest",
    RegionSrvMinVer: "9.3.1",
    RegionSrvEnablerService: "regionsrv-enabler-azure.service",
    RegionSrv: "cloud-regionsrv-client",
    RegionSrvAddOn: "cloud-regionsrv-client-addon-azure",
    AddonPath: "/usrb/sbin/regionsrv-enabler-azure",
    RepoAlias: "sle-ahb-packages",
    ModName: "sle-module-public-cloud",
    RepoUrl: "https://updates.suse.com/SUSE/Updates/SLE-Module-Public-Cloud-Unrestricted/%s/x86_64/update",
  }
  //1. double check that the regionsrv-enabler-azure.service file exists
  status := "success"
  _, err := os.Stat(s.AddonPath)
  if err != nil {
    return "failure", err
  }
  //2. enable the service
  _, err = exec.Command("systemctl", "enable", s.RegionSrvEnablerService).Output()
  if err != nil {
    fmt.Println("Error when enabling repo", s.RegionSrvEnablerService)
    status = "failure"
  }

  return status, err // return "put your extension code here", nil
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

//func main() {
//	err := getExtensionAndRun()
//	if err != nil {
//		os.Exit(exithelper.EnvironmentError)
//	}
//}

func getExtensionAndRun() error {
	initilizationInfo, err := getInitializationInfoFuncToCall(extensionName, extensionVersion, true, enableCallbackFunc)
	if err != nil {
		return err
	}

	initilizationInfo.DisableCallback = disableCallbackFunc
	initilizationInfo.UpdateCallback = updateCallbackFunc
	vmExt, err := getVMExtensionFuncToCall(initilizationInfo)
	if err != nil {
		return err
	}
	vmExt.Do()
	return nil
}