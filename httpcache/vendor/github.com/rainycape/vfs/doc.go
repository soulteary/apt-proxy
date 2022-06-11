// Package vfs implements Virtual File Systems with read-write support.
//
// All implementatations use slash ('/') separated paths, with / representing
// the root directory. This means that to manipulate or construct paths, the
// functions in path package should be used, like path.Join or path.Dir.
// There's also no notion of the current directory nor relative paths. The paths
// /a/b/c and a/b/c are considered to point to the same element.
//
// This package also implements some shorthand functions which might be used with
// any VFS implementation, providing the same functionality than functions in the
// io/ioutil, os and path/filepath packages:
//
//  io/ioutil.ReadFile => ReadFile
//  io/ioutil.WriteFile => WriteFile
//  os.IsExist => IsExist
//  os.IsNotExist => IsNotExist
//  os.MkdirAll => MkdirAll
//  os.RemoveAll => RemoveAll
//  path/filepath.Walk => Walk
//
// All VFS implementations are thread safe, so multiple readers and writers might
// operate on them at any time.
package vfs
