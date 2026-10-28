package main

import (
	"fmt"
	"net/url"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

// httpGet uses the system WinHTTP client instead of net/http, which keeps Go's TLS stack
// out of the Windows binary; WinHTTP also applies the system proxy and certificate store.
var (
	winhttp             = windows.NewLazySystemDLL("winhttp.dll")
	procOpen            = winhttp.NewProc("WinHttpOpen")
	procConnect         = winhttp.NewProc("WinHttpConnect")
	procOpenRequest     = winhttp.NewProc("WinHttpOpenRequest")
	procSendRequest     = winhttp.NewProc("WinHttpSendRequest")
	procReceiveResponse = winhttp.NewProc("WinHttpReceiveResponse")
	procQueryHeaders    = winhttp.NewProc("WinHttpQueryHeaders")
	procReadData        = winhttp.NewProc("WinHttpReadData")
	procCloseHandle     = winhttp.NewProc("WinHttpCloseHandle")
)

const (
	accessTypeAutomaticProxy = 4
	flagSecure               = 0x00800000
	queryStatusCode          = 19
	queryFlagNumber          = 0x20000000
)

func wstr(s string) uintptr {
	p, _ := windows.UTF16PtrFromString(s)
	return uintptr(unsafe.Pointer(p))
}

func call(p *windows.LazyProc, args ...uintptr) (uintptr, error) {
	r, _, err := p.Call(args...)
	if r == 0 {
		return 0, err
	}
	return r, nil
}

func httpGet(target, accept string) ([]byte, int, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, 0, err
	}
	port, flags := 80, uintptr(0)
	if u.Scheme == "https" {
		port, flags = 443, flagSecure
	}
	if p := u.Port(); p != "" {
		if port, err = strconv.Atoi(p); err != nil {
			return nil, 0, err
		}
	}
	path := u.RequestURI()

	fail := func(step string, err error) ([]byte, int, error) {
		return nil, 0, fmt.Errorf("Failed to fetch %s: %s: %v", target, step, err)
	}
	session, err := call(procOpen, wstr("spring-init"), accessTypeAutomaticProxy, 0, 0, 0)
	if err != nil {
		return fail("WinHttpOpen", err)
	}
	defer procCloseHandle.Call(session)
	conn, err := call(procConnect, session, wstr(u.Hostname()), uintptr(port), 0)
	if err != nil {
		return fail("connect", err)
	}
	defer procCloseHandle.Call(conn)
	req, err := call(procOpenRequest, conn, wstr("GET"), wstr(path), 0, 0, 0, flags)
	if err != nil {
		return fail("open request", err)
	}
	defer procCloseHandle.Call(req)
	if _, err := call(procSendRequest, req, wstr("Accept: "+accept), ^uintptr(0), 0, 0, 0, 0); err != nil {
		return fail("send request", err)
	}
	if _, err := call(procReceiveResponse, req, 0); err != nil {
		return fail("receive response", err)
	}
	var status, size uint32 = 0, 4
	if _, err := call(procQueryHeaders, req, queryStatusCode|queryFlagNumber, 0,
		uintptr(unsafe.Pointer(&status)), uintptr(unsafe.Pointer(&size)), 0); err != nil {
		return fail("read status", err)
	}
	var body []byte
	buf := make([]byte, 64*1024)
	for {
		var n uint32
		if _, err := call(procReadData, req, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n))); err != nil {
			return fail("read body", err)
		}
		if n == 0 {
			return body, int(status), nil
		}
		body = append(body, buf[:n]...)
	}
}
