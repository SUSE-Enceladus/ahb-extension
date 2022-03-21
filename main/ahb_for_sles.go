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
  "fmt"
	"os"
  "os/exec"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/go-kit/kit/log"
)

const (
	extensionName    = "AHBForSLES"
	extensionVersion = "0.0.0.1"
)

type Public_Cloud_Info struct {
  PublicCloudService             string
  RegisterCloudGuestPath         string
  RegionSrvMinVer                string
  RegionSrvEnablerService        string
  RegionSrv                      string
  RegionSrvAddOn                 string
  AddonPath                      string
  RepoAlias                      string
  ModName                        string
  RepoUrl                        string
}


var enableCallbackFunc vmextension.EnableCallbackFunc = func(ext *vmextension.VMExtension) (string, error) {
	pub_cloud_info := Public_Cloud_Info{
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
  _, err := os.Stat(pub_cloud_info.AddonPath)
  if err != nil {
    return "failure", err
  }
  //2. enable the service
  _, err = exec.Command("systemctl", "enable", pub_cloud_info.RegionSrvEnablerService).Output()
  if err != nil {
    fmt.Println("Error when enabling repo", pub_cloud_info.RegionSrvEnablerService)
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
