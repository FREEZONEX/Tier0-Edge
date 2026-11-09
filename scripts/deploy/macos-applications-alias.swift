import AppKit

// A Finder alias carries its own icon. A bare POSIX symlink relies on the host's
// Applications icon lookup, which can render as an empty placeholder in Finder.
let output = URL(fileURLWithPath: CommandLine.arguments[1])
let target = URL(fileURLWithPath: "/Applications", isDirectory: true)
guard !FileManager.default.fileExists(atPath: output.path) else {
    fatalError("Applications alias output already exists")
}
let bookmark = try target.bookmarkData(options: [.suitableForBookmarkFile], includingResourceValuesForKeys: nil, relativeTo: nil)
try URL.writeBookmarkData(bookmark, to: output)

// Self-contained folder artwork: independent of host IconServices caches and
// OS-specific lazy system icons. The real alias remains the draggable target.
let icon = NSImage(size: NSSize(width: 512, height: 512))
icon.lockFocus()
NSColor(srgbRed: 0.20, green: 0.58, blue: 0.88, alpha: 1).setFill()
NSBezierPath(roundedRect: NSRect(x: 36, y: 98, width: 440, height: 290), xRadius: 28, yRadius: 28).fill()
NSBezierPath(roundedRect: NSRect(x: 36, y: 334, width: 188, height: 86), xRadius: 24, yRadius: 24).fill()
let front = NSBezierPath(roundedRect: NSRect(x: 36, y: 78, width: 440, height: 286), xRadius: 28, yRadius: 28)
NSGradient(starting: NSColor(srgbRed: 0.38, green: 0.78, blue: 1, alpha: 1), ending: NSColor(srgbRed: 0.12, green: 0.55, blue: 0.91, alpha: 1))!.draw(in: front, angle: -90)
let mark = NSBezierPath()
mark.lineWidth = 24
mark.lineCapStyle = .round
mark.move(to: NSPoint(x: 190, y: 150)); mark.line(to: NSPoint(x: 278, y: 305))
mark.move(to: NSPoint(x: 234, y: 305)); mark.line(to: NSPoint(x: 322, y: 150))
mark.move(to: NSPoint(x: 173, y: 194)); mark.line(to: NSPoint(x: 339, y: 194))
NSColor.white.withAlphaComponent(0.92).setStroke(); mark.stroke()
icon.unlockFocus()
guard NSWorkspace.shared.setIcon(icon, forFile: output.path, options: []) else {
    fatalError("Cannot embed Applications alias icon")
}
let resolved = try URL(resolvingAliasFileAt: output, options: [.withoutUI, .withoutMounting])
guard resolved.path == "/Applications" else { fatalError("Incorrect Applications alias target") }
print("Finder Applications alias with embedded icon created")
