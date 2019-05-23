package netadmit

import (
  "errors"
  "net"
  "encoding/binary"
  admissionv1 "k8s.io/api/admission/v1beta1"
  danmtypes "github.com/nokia/danm/crd/apis/danm/v1"
  "github.com/nokia/danm/pkg/ipam"
)

type Validator func(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation) error

type ValidatorMapping []Validator


const (
  MaxNidLength = 12
)

var (
  DanmNetMapping = []Validator{validateIpv4Fields,validateIpv6Fields,validateAllocationPool,validateVids,validateNetworkId,validateAbsenceOfAllowedTenants}
  ClusterNetMapping = []Validator{validateIpv4Fields,validateIpv6Fields,validateAllocationPool,validateVids,validateNetworkId}
  TenantNetMapping = []Validator{validateIpv4Fields,validateIpv6Fields,validateAllocationPool,validateNetworkId,validateAbsenceOfAllowedTenants,validateTenantNetRules}
  danmValidationConfig = map[string]ValidatorMapping {
    "DanmNet": DanmNetMapping,
    "ClusterNetwork": ClusterNetMapping,
    "TenantNetwork": TenantNetMapping,
  }
)

func validateIpv4Fields(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation) error {
  return validateIpFields(newManifest.Spec.Options.Cidr, newManifest.Spec.Options.Routes)
}

func validateIpv6Fields(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation) error {
  return validateIpFields(newManifest.Spec.Options.Net6, newManifest.Spec.Options.Routes6)
}

func validateIpFields(cidr string, routes map[string]string) error {
  if cidr == "" {
    if routes != nil  {
      return errors.New("IP routes cannot be defined for a L2 network")
    }
    return nil
  }
  _, ipnet, err := net.ParseCIDR(cidr)
  if err != nil {
    return errors.New("Invalid CIDR: " + cidr)
  }
  for _, gw := range routes {
    if !ipnet.Contains(net.ParseIP(gw)) {
      return errors.New("Specified GW address:" + gw + " is not part of CIDR:" + cidr)
    }
  }
  return nil
}

func validateAllocationPool(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation) error {
  if opType == admissionv1.Create && newManifest.Spec.Options.Alloc != "" {
    return errors.New("Allocation bitmask shall not be manually defined upon creation!")  
  }
  cidr := newManifest.Spec.Options.Cidr
  if cidr == "" {
    if newManifest.Spec.Options.Pool.Start != "" || newManifest.Spec.Options.Pool.End != "" {
      return errors.New("Allocation pool cannot be defined without CIDR!")
    }
    return nil
  }
  _, ipnet, err := net.ParseCIDR(cidr)
  if err != nil {
    return errors.New("Invalid CIDR parameter: " + cidr)
  }
  if newManifest.Spec.Options.Pool.Start == "" {
    newManifest.Spec.Options.Pool.Start = (ipam.Int2ip(ipam.Ip2int(ipnet.IP) + 1)).String()
  }
  if newManifest.Spec.Options.Pool.End == "" {
    newManifest.Spec.Options.Pool.End = (ipam.Int2ip(ipam.Ip2int(GetBroadcastAddress(ipnet)) - 1)).String()
  }
  if !ipnet.Contains(net.ParseIP(newManifest.Spec.Options.Pool.Start)) || !ipnet.Contains(net.ParseIP(newManifest.Spec.Options.Pool.End)) {
    return errors.New("Allocation pool is outside of defined CIDR")
  }
  if ipam.Ip2int(net.ParseIP(newManifest.Spec.Options.Pool.End)) <= ipam.Ip2int(net.ParseIP(newManifest.Spec.Options.Pool.Start)) {
    return errors.New("Allocation pool start:" + newManifest.Spec.Options.Pool.Start + " is bigger than or equal to allocation pool end:" + newManifest.Spec.Options.Pool.End)
  }
  return nil
}

func GetBroadcastAddress(subnet *net.IPNet) (net.IP) {
  ip := make(net.IP, len(subnet.IP.To4()))
  //Don't ask
  binary.BigEndian.PutUint32(ip, binary.BigEndian.Uint32(subnet.IP.To4())|^binary.BigEndian.Uint32(net.IP(subnet.Mask).To4()))
  return ip
}

func validateVids(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation) error {
  isVlanDefined := (newManifest.Spec.Options.Vlan!=0)
  isVxlanDefined := (newManifest.Spec.Options.Vxlan!=0)
  if isVlanDefined && isVxlanDefined {
    return errors.New("VLAN ID and VxLAN ID parameters are mutually exclusive")
  }
  return nil
}

func validateNetworkId(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation) error {
  if newManifest.Spec.NetworkID == "" {
    return errors.New("Spec.NetworkID mandatory parameter is missing!")
  }
  if len(newManifest.Spec.NetworkID) > MaxNidLength {
    return errors.New("Spec.NetworkID cannot be longer than 12 characters (otherwise VLAN and VxLAN host interface creation might fail)!")
  }
  return nil
}

func validateAbsenceOfAllowedTenants(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation) error {
  if newManifest.Spec.AllowedTenants != nil {
    return errors.New("AllowedTenants attribute is only valid for the ClusterNetwork API!")
  }
  return nil
}

func validateTenantNetRules(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation) error {
  if opType == admissionv1.Create && 
    (newManifest.Spec.Options.Device != "" ||
     newManifest.Spec.Options.Vxlan  != 0  ||
     newManifest.Spec.Options.Vlan   != 0) {
    return errors.New("Manually configuring any one of host_device, vlan, or vxlan attributes is not allowed for TenantNetworks!")  
  }
  if opType == admissionv1.Update && 
    (newManifest.Spec.Options.Device  != oldManifest.Spec.Options.Device  || 
     newManifest.Spec.Options.Vxlan   != oldManifest.Spec.Options.Vxlan   ||
     newManifest.Spec.Options.Vlan    != oldManifest.Spec.Options.Vlan) {
    return errors.New("Manually changing any one of host_device, vlan, or vxlan attributes is not allowed for TenantNetworks!")  
  }
  return nil
}