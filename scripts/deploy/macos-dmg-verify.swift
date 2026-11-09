import AppKit
import CoreFoundation

// Exercise the system alias resolver after the compressed DMG has been remounted.
// The legacy conversion is required because Finder's icvp uses Carbon alias data.
let alias = Data(base64Encoded: CommandLine.arguments[1])!
let bookmark = CFURLCreateBookmarkDataFromAliasRecord(nil, alias as CFData)!.takeRetainedValue() as Data
var stale = false
let resolved = try URL(resolvingBookmarkData: bookmark, options: [.withoutUI, .withoutMounting], bookmarkDataIsStale: &stale)
let expected = URL(fileURLWithPath: CommandLine.arguments[2]).resolvingSymlinksInPath()
guard resolved.resolvingSymlinksInPath() == expected,
      let image = NSImage(contentsOf: resolved), image.size.width == 760, image.size.height == 500 else {
    fatalError("Final DMG background must resolve and decode from the mounted volume")
}
print("System alias resolution and background image verified: \(resolved.path)")
let applications = expected.deletingLastPathComponent().deletingLastPathComponent().appendingPathComponent("Applications")
let destination = try URL(resolvingAliasFileAt: applications, options: [.withoutUI, .withoutMounting])
guard destination.path == "/Applications",
      (try applications.resourceValues(forKeys: [.isAliasFileKey])).isAliasFile == true,
      let resourceFork = FileHandle(forReadingAtPath: applications.path + "/..namedfork/rsrc"),
      (try resourceFork.seekToEnd()) > 0 else {
    fatalError("Final DMG must preserve the Applications Finder alias and its custom icon")
}
try resourceFork.close()
print("Applications Finder alias resolves to /Applications; embedded icon preserved")
