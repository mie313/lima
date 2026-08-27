// SPDX-FileCopyrightText: Copyright The Lima Authors
// SPDX-License-Identifier: Apache-2.0

package hcs

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------------------------------------------------------------------------
// computecore.dll (HCS) bindings
// ---------------------------------------------------------------------------

var (
	modcomputecore = windows.NewLazySystemDLL("computecore.dll")

	procHcsCreateOperation            = modcomputecore.NewProc("HcsCreateOperation")
	procHcsCloseOperation             = modcomputecore.NewProc("HcsCloseOperation")
	procHcsWaitForOperationResult     = modcomputecore.NewProc("HcsWaitForOperationResult")
	procHcsCreateComputeSystem        = modcomputecore.NewProc("HcsCreateComputeSystem")
	procHcsStartComputeSystem         = modcomputecore.NewProc("HcsStartComputeSystem")
	procHcsTerminateComputeSystem     = modcomputecore.NewProc("HcsTerminateComputeSystem")
	procHcsCloseComputeSystem         = modcomputecore.NewProc("HcsCloseComputeSystem")
	procHcsGrantVmAccess              = modcomputecore.NewProc("HcsGrantVmAccess")
	procHcsOpenComputeSystem          = modcomputecore.NewProc("HcsOpenComputeSystem")
	procHcsWaitForComputeSystemExit   = modcomputecore.NewProc("HcsWaitForComputeSystemExit")
	procHcsGetComputeSystemProperties = modcomputecore.NewProc("HcsGetComputeSystemProperties")
	procShutDownComputeSystem         = modcomputecore.NewProc("ShutDownComputeSystem")
)

type (
	hcsOperation syscall.Handle
	hcsSystem    syscall.Handle
)

// hresultErr converts an HRESULT return value into a Go error, unwrapping
// FACILITY_WIN32 the same way hcsshim's generated bindings do.
func hresultErr(r0 uintptr) error {
	if int32(r0) >= 0 {
		return nil
	}
	if r0&0x1fff0000 == 0x00070000 {
		r0 &= 0xffff
	}
	return syscall.Errno(r0)
}

// coString copies a CoTaskMem-allocated PWSTR into a Go string and frees it.
func coString(p *uint16) string {
	if p == nil {
		return ""
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}

func hcsCreateOperation() (hcsOperation, error) {
	r0, _, e1 := syscall.SyscallN(procHcsCreateOperation.Addr(), 0, 0)
	if r0 == 0 {
		return 0, fmt.Errorf("HcsCreateOperation: %w", e1)
	}
	return hcsOperation(r0), nil
}

func hcsCloseOperation(op hcsOperation) {
	_, _, _ = syscall.SyscallN(procHcsCloseOperation.Addr(), uintptr(op))
}

// hcsWait drives one async HCS call to completion and returns the result
// document (which carries structured error info on failure).
func hcsWait(op hcsOperation, what string, callErr error) (string, error) {
	if callErr != nil {
		return "", fmt.Errorf("%s: %w", what, callErr)
	}
	var result *uint16
	r0, _, _ := syscall.SyscallN(procHcsWaitForOperationResult.Addr(),
		uintptr(op), uintptr(infiniteTimeout), uintptr(unsafe.Pointer(&result)))
	doc := coString(result)
	if err := hresultErr(r0); err != nil {
		return doc, fmt.Errorf("%s: %w (result: %s)", what, err, doc)
	}
	return doc, nil
}

func hcsCreateComputeSystem(id, configuration string) (hcsSystem, error) {
	op, err := hcsCreateOperation()
	if err != nil {
		return 0, err
	}
	defer hcsCloseOperation(op)

	idP, err := syscall.UTF16PtrFromString(id)
	if err != nil {
		return 0, err
	}
	cfgP, err := syscall.UTF16PtrFromString(configuration)
	if err != nil {
		return 0, err
	}
	var system hcsSystem
	r0, _, _ := syscall.SyscallN(procHcsCreateComputeSystem.Addr(),
		uintptr(unsafe.Pointer(idP)), uintptr(unsafe.Pointer(cfgP)),
		uintptr(op), 0, uintptr(unsafe.Pointer(&system)))
	if _, err := hcsWait(op, "HcsCreateComputeSystem", hresultErr(r0)); err != nil {
		return 0, err
	}
	return system, nil
}

func hcsStartComputeSystem(system hcsSystem) error {
	op, err := hcsCreateOperation()
	if err != nil {
		return err
	}
	defer hcsCloseOperation(op)
	r0, _, _ := syscall.SyscallN(procHcsStartComputeSystem.Addr(),
		uintptr(system), uintptr(op), 0)
	_, err = hcsWait(op, "HcsStartComputeSystem", hresultErr(r0))
	return err
}

func hcsTerminateComputeSystem(system hcsSystem) error {
	op, err := hcsCreateOperation()
	if err != nil {
		return err
	}
	defer hcsCloseOperation(op)
	r0, _, _ := syscall.SyscallN(procHcsTerminateComputeSystem.Addr(),
		uintptr(system), uintptr(op), 0)
	_, err = hcsWait(op, "HcsTerminateComputeSystem", hresultErr(r0))
	return err
}

func hcsCloseComputeSystem(system hcsSystem) {
	_, _, _ = syscall.SyscallN(procHcsCloseComputeSystem.Addr(), uintptr(system))
}

// hcsGrantVmAccess ACLs a host file so the VM worker process may read it
// (hcsshim does the equivalent before every kernel-direct boot).
func hcsGrantVmAccess(vmID, path string) error {
	idP, err := syscall.UTF16PtrFromString(vmID)
	if err != nil {
		return err
	}
	pathP, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r0, _, _ := syscall.SyscallN(procHcsGrantVmAccess.Addr(),
		uintptr(unsafe.Pointer(idP)), uintptr(unsafe.Pointer(pathP)))
	return hresultErr(r0)
}

// ---------------------------------------------------------------------------
// computenetwork.dll (HCN) bindings
// ---------------------------------------------------------------------------

var (
	modcomputenetwork = windows.NewLazySystemDLL("computenetwork.dll")

	procHcnCreateNetwork  = modcomputenetwork.NewProc("HcnCreateNetwork")
	procHcnCloseNetwork   = modcomputenetwork.NewProc("HcnCloseNetwork")
	procHcnDeleteNetwork  = modcomputenetwork.NewProc("HcnDeleteNetwork")
	procHcnCreateEndpoint = modcomputenetwork.NewProc("HcnCreateEndpoint")
	procHcnCloseEndpoint  = modcomputenetwork.NewProc("HcnCloseEndpoint")
	procHcnDeleteEndpoint = modcomputenetwork.NewProc("HcnDeleteEndpoint")
	procHcnQueryEndpoint  = modcomputenetwork.NewProc("HcnQueryEndpointProperties")
)

type (
	hcnNetwork  syscall.Handle
	hcnEndpoint syscall.Handle
)

// hcnErr folds an HRESULT plus HCN's JSON error record into one error.
func hcnErr(what string, r0 uintptr, record *uint16) error {
	rec := coString(record)
	if err := hresultErr(r0); err != nil {
		return fmt.Errorf("%s: %w (%s)", what, err, rec)
	}
	return nil
}

func hcnCreateNetwork(id *windows.GUID, settings string) (hcnNetwork, error) {
	sP, err := syscall.UTF16PtrFromString(settings)
	if err != nil {
		return 0, err
	}
	var network hcnNetwork
	var record *uint16
	r0, _, _ := syscall.SyscallN(procHcnCreateNetwork.Addr(),
		uintptr(unsafe.Pointer(id)), uintptr(unsafe.Pointer(sP)),
		uintptr(unsafe.Pointer(&network)), uintptr(unsafe.Pointer(&record)))
	return network, hcnErr("HcnCreateNetwork", r0, record)
}

func hcnCloseNetwork(network hcnNetwork) {
	_, _, _ = syscall.SyscallN(procHcnCloseNetwork.Addr(), uintptr(network))
}

func hcnDeleteNetwork(id *windows.GUID) error {
	var record *uint16
	r0, _, _ := syscall.SyscallN(procHcnDeleteNetwork.Addr(),
		uintptr(unsafe.Pointer(id)), uintptr(unsafe.Pointer(&record)))
	return hcnErr("HcnDeleteNetwork", r0, record)
}

func hcnCreateEndpoint(network hcnNetwork, id *windows.GUID, settings string) (hcnEndpoint, error) {
	sP, err := syscall.UTF16PtrFromString(settings)
	if err != nil {
		return 0, err
	}
	var endpoint hcnEndpoint
	var record *uint16
	r0, _, _ := syscall.SyscallN(procHcnCreateEndpoint.Addr(),
		uintptr(network), uintptr(unsafe.Pointer(id)), uintptr(unsafe.Pointer(sP)),
		uintptr(unsafe.Pointer(&endpoint)), uintptr(unsafe.Pointer(&record)))
	return endpoint, hcnErr("HcnCreateEndpoint", r0, record)
}

func hcnCloseEndpoint(endpoint hcnEndpoint) {
	_, _, _ = syscall.SyscallN(procHcnCloseEndpoint.Addr(), uintptr(endpoint))
}

func hcnDeleteEndpoint(id *windows.GUID) error {
	var record *uint16
	r0, _, _ := syscall.SyscallN(procHcnDeleteEndpoint.Addr(),
		uintptr(unsafe.Pointer(id)), uintptr(unsafe.Pointer(&record)))
	return hcnErr("HcnDeleteEndpoint", r0, record)
}

func hcnQueryEndpointProperties(endpoint hcnEndpoint) (string, error) {
	query := `{"SchemaVersion":{"Major":2,"Minor":0}}`
	qP, err := syscall.UTF16PtrFromString(query)
	if err != nil {
		return "", err
	}
	var props, record *uint16
	r0, _, _ := syscall.SyscallN(procHcnQueryEndpoint.Addr(),
		uintptr(endpoint), uintptr(unsafe.Pointer(qP)),
		uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&record)))
	doc := coString(props)
	return doc, hcnErr("HcnQueryEndpointProperties", r0, record)
}

// ---------------------------------------------------------------------------
// GUIDs
// ---------------------------------------------------------------------------

func newGUID() windows.GUID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		log.Fatalf("rand: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return windows.GUID{
		Data1: uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]),
		Data2: uint16(b[4])<<8 | uint16(b[5]),
		Data3: uint16(b[6])<<8 | uint16(b[7]),
		Data4: [8]byte{b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15]},
	}
}

func guidString(g windows.GUID) string {
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		g.Data1, g.Data2, g.Data3,
		g.Data4[0], g.Data4[1], g.Data4[2], g.Data4[3],
		g.Data4[4], g.Data4[5], g.Data4[6], g.Data4[7])
}

// ---------------------------------------------------------------------------
// JSON documents (HCN v2 network/endpoint, HCS schema-2 compute system)
// ---------------------------------------------------------------------------

// HNS network flags, from hns::NetworkFlags in microsoft/WSL's
// src/shared/inc/hns_schema.h. EnableDhcp|EnableDns put HNS's built-in
// DHCP server and DNS proxy on the network's gateway address. The Hyper-V
// Default Switch runs with EnableDns|EnableDhcp|EnableNonPersistent; WSL's
// NAT mode runs with EnableDns|EnableNonPersistent and pushes static
// addresses instead of DHCP.
const (
	netFlagEnableDns           = 1
	netFlagEnableDhcp          = 2
	netFlagEnableNonPersistent = 8
)

// icsNetworkJSON builds an ICS network document in the same V1
// (hns::Network) shape WSL passes to HcnCreateNetwork. HNS and the ICS
// service create the vSwitch, a host vNIC holding the gateway address
// (first host of the subnet), NAT to the host's Internet connection, and —
// because of EnableDhcp|EnableDns — a DHCP server and DNS proxy on the
// gateway.
func icsNetworkJSON(name, subnet, gateway string) string {
	doc := map[string]any{
		"Name":          name,
		"Type":          "ICS",
		"IsolateSwitch": true,
		"Flags":         netFlagEnableDns | netFlagEnableDhcp | netFlagEnableNonPersistent,
		"Subnets": []any{
			map[string]any{
				"GatewayAddress": gateway,
				"AddressPrefix":  subnet,
				"IpSubnets": []any{
					map[string]any{"IpAddressPrefix": subnet},
				},
			},
		},
	}
	b, _ := json.Marshal(doc)
	return string(b)
}

func endpointJSON(networkID string) string {
	doc := map[string]any{
		"SchemaVersion":      map[string]int{"Major": 2, "Minor": 0},
		"HostComputeNetwork": networkID,
		"Policies":           []any{},
	}
	b, _ := json.Marshal(doc)
	return string(b)
}

// endpointProps is the slice of HCN endpoint properties this program
// consumes: the MAC (mirrored into the compute system document's
// NetworkAdapter, as in microsoft/WSL's hcs_schema and hcsshim's UVM
// attach) and, when present, the HNS-assigned IP (logged as the expected
// DHCP lease).
type endpointProps struct {
	MacAddress       string `json:"MacAddress"`
	IPConfigurations []struct {
		IPAddress    string `json:"IpAddress"`
		PrefixLength int    `json:"PrefixLength"`
	} `json:"IpConfigurations"`
}

type vmParams struct {
	kernel     string
	initrd     string
	cmdline    string
	vhdx       string
	cpus       uint32
	memMB      uint64
	comPipe    string
	endpointID string
	macAddress string
}

// computeSystemJSON builds the schema-2 document. Schema 2.2 is the minimum
// for Chipset.LinuxKernelDirect. The NetworkAdapter carries exactly
// {EndpointId, MacAddress}, the only NIC form the HCS schema accepts.
func computeSystemJSON(p vmParams) string {
	devices := map[string]any{
		"ComPorts": map[string]any{
			"0": map[string]any{"NamedPipe": p.comPipe},
		},
		"NetworkAdapters": map[string]any{
			p.endpointID: map[string]any{
				"EndpointId": p.endpointID,
				"MacAddress": p.macAddress,
			},
		},
	}
	if p.vhdx != "" {
		devices["Scsi"] = map[string]any{
			"0": map[string]any{
				"Attachments": map[string]any{
					"0": map[string]any{"Type": "VirtualDisk", "Path": p.vhdx},
				},
			},
		}
	}
	doc := map[string]any{
		"Owner":                             "hcsvm",
		"SchemaVersion":                     map[string]int{"Major": 2, "Minor": 2},
		"ShouldTerminateOnLastHandleClosed": true,
		"VirtualMachine": map[string]any{
			"StopOnReset": true,
			"Chipset": map[string]any{
				"LinuxKernelDirect": map[string]any{
					"KernelFilePath": p.kernel,
					"InitRdPath":     p.initrd,
					"KernelCmdLine":  p.cmdline,
				},
			},
			"ComputeTopology": map[string]any{
				"Memory": map[string]any{
					"SizeInMB":        p.memMB,
					"AllowOvercommit": true,
				},
				"Processor": map[string]any{"Count": p.cpus},
			},
			"Devices": devices,
		},
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	return string(b)
}

// ---------------------------------------------------------------------------
// Serial console: streaming + a minimal expect engine for -netcheck
// ---------------------------------------------------------------------------

const (
	ifUpMarker    = "HCSVM_IF_UP"
	ifFailMarker  = "HCSVM_IF_FAIL"
	netOKMarker   = "HCSVM_NET_OK"
	netFailMarker = "HCSVM_NET_FAIL"
)

// netcheckSteps drives the stock initramfs over the serial console. Booted
// with no root media, Alpine's init drops to its recovery shell; there we
// load hv_netvsc and run udhcpc — the same client the initramfs itself
// uses for ip=dhcp netboots — whose /usr/share/udhcpc/default.script
// applies the address, default route, and resolv.conf leased by the HNS
// DHCP server on the gateway. With -dns non-empty, resolv.conf is then
// overwritten with those servers instead of the DHCP-provided ones. Then
// curl is installed with apk (itself an HTTPS fetch through the NAT) and
// the URL fetched.
//
// Markers are written quoted in the commands (HCSVM_NET_"OK") so the tty's
// echo of our own input can never satisfy the matcher; only real command
// output produces the contiguous marker string.
func netcheckSteps(dns []string, url string) []expectStep {
	resolv := ""
	for _, d := range dns {
		if d != "" {
			resolv += "nameserver " + d + `\n`
		}
	}
	dnsOverride := ""
	if resolv != "" {
		dnsOverride = fmt.Sprintf("printf '%s' > /etc/resolv.conf; ", resolv)
	}
	ifup := "modprobe hv_netvsc; ip link set eth0 up; " +
		"udhcpc -i eth0 -t 10 -T 3 -n -q && { " + dnsOverride +
		`echo HCSVM_IF_"UP"; } || echo HCSVM_IF_"FAIL"` + "\n"
	check := `apk -q --initdb -X https://dl-cdn.alpinelinux.org/alpine/latest-stable/main -U add curl ca-certificates && ` +
		`curl -fsS -o /dev/null ` + url +
		` && echo HCSVM_NET_"OK" || echo HCSVM_NET_"FAIL"` + "\n"
	return []expectStep{
		{waitFor: "emergency recovery shell", send: "\n" + ifup},
		{waitFor: ifUpMarker, failOn: ifFailMarker, send: check},
		{waitFor: netOKMarker, failOn: netFailMarker},
	}
}

type expectStep struct {
	waitFor string // substring of serial output to wait for
	failOn  string // optional substring that fails the sequence immediately
	send    string // written to the console once waitFor is seen
}

// serialSession attaches to the VM's COM1 named pipe (served by the VM
// worker process), mirrors everything to stdout, and walks the expect
// steps. The result channel receives nil when all steps completed, or an
// error.
func serialSession(pipe string, steps []expectStep, result chan<- error) {
	f, err := openPipeRetry(pipe)
	if err != nil {
		result <- err
		return
	}
	defer f.Close()
	log.Printf("serial console attached (%s)", pipe)

	var tail []byte
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			_, _ = os.Stdout.Write(buf[:n])
			tail = append(tail, buf[:n]...)
			if len(tail) > 8192 {
				tail = tail[len(tail)-8192:]
			}
			for len(steps) > 0 {
				step := steps[0]
				if step.failOn != "" && strings.Contains(string(tail), step.failOn) {
					result <- fmt.Errorf("guest reported %q", step.failOn)
					steps = nil
					break
				}
				if !strings.Contains(string(tail), step.waitFor) {
					break
				}
				log.Printf("serial: matched %q", step.waitFor)
				tail = nil
				if step.send != "" {
					time.Sleep(300 * time.Millisecond)
					if _, werr := f.Write([]byte(step.send)); werr != nil {
						result <- fmt.Errorf("serial write: %w", werr)
						return
					}
				}
				steps = steps[1:]
				if len(steps) == 0 {
					result <- nil
				}
			}
		}
		if err != nil {
			if len(steps) > 0 {
				result <- fmt.Errorf("serial closed before completing (waiting for %q): %w",
					steps[0].waitFor, err)
			}
			return
		}
	}
}

func openPipeRetry(pipe string) (*os.File, error) {
	for i := 0; i < 100; i++ {
		f, err := os.OpenFile(pipe, os.O_RDWR, 0)
		if err == nil {
			return f, nil
		}
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, windows.ERROR_PIPE_BUSY) {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		return nil, fmt.Errorf("serial: %w", err)
	}
	return nil, fmt.Errorf("serial: pipe %s never appeared", pipe)
}
