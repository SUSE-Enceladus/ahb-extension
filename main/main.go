// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.
package main

import (
	"github.com/Azure/azure-extension-platform/extensionlauncher"
	"github.com/Azure/azure-extension-platform/pkg/exithelper"
	"github.com/Azure/azure-extension-platform/pkg/handlerenv"
	"github.com/Azure/azure-extension-platform/pkg/logging"
)

var el = logging.New(nil)
var eh = exithelper.Exiter
type SLES struct {
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

func main() {
  sles := SLES{
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

	extName, extVersion, exeName, operation, err := extensionlauncher.ParseArgs()
	if err != nil {
		el.Error("error parsing arguments %s", err.Error())
		eh.Exit(exithelper.ArgumentError)
	}
	handlerEnv, err := handlerenv.GetHandlerEnvironment(extName, extVersion)
	if err != nil {
		el.Error("could not retrieve handler environment %s", err.Error())
		eh.Exit(exithelper.EnvironmentError)
	}
	el = logging.New(handlerEnv)
	extensionlauncher.Run(handlerEnv, el, extName, extVersion, exeName, operation)
	eh.Exit(0)
}
