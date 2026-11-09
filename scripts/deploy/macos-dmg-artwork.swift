import AppKit

// Build-time artwork. Finder supplies the real draggable icons and labels.
let output = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
let favicon = URL(fileURLWithPath: CommandLine.arguments[2])
let fm = FileManager.default
try fm.createDirectory(at: output, withIntermediateDirectories: true)
func color(_ r: CGFloat, _ g: CGFloat, _ b: CGFloat) -> NSColor {
    NSColor(srgbRed: r / 255, green: g / 255, blue: b / 255, alpha: 1)
}
func rect(_ x: CGFloat, _ y: CGFloat, _ w: CGFloat, _ h: CGFloat) -> NSRect {
    NSRect(x: x, y: 500 - y - h, width: w, height: h)
}
func text(_ value: String, _ x: CGFloat, _ y: CGFloat, _ w: CGFloat, _ h: CGFloat,
          _ size: CGFloat, _ weight: NSFont.Weight, _ ink: NSColor, centered: Bool = false) {
    let paragraph = NSMutableParagraphStyle()
    paragraph.alignment = centered ? .center : .left
    paragraph.lineSpacing = 4
    (value as NSString).draw(in: rect(x, y, w, h), withAttributes: [
        .font: NSFont.systemFont(ofSize: size, weight: weight), .foregroundColor: ink,
        .paragraphStyle: paragraph
    ])
}
let image = NSImage(size: NSSize(width: 760, height: 500))
image.lockFocus()
NSColor.white.setFill(); NSRect(x: 0, y: 0, width: 760, height: 500).fill()
let ink = color(25, 31, 36), muted = color(105, 113, 120), green = color(142, 198, 28)
text("安装 Tier0 Edge", 40, 36, 680, 48, 32, .semibold, ink, centered: true)
text("拖入应用程序，即可开始使用", 40, 88, 680, 28, 16, .regular, muted, centered: true)
let arrow = NSBezierPath(); arrow.lineWidth = 7; arrow.lineCapStyle = .round; arrow.lineJoinStyle = .round
arrow.move(to: NSPoint(x: 340, y: 295)); arrow.line(to: NSPoint(x: 420, y: 295))
arrow.move(to: NSPoint(x: 403, y: 312)); arrow.line(to: NSPoint(x: 420, y: 295)); arrow.line(to: NSPoint(x: 403, y: 278))
green.setStroke(); arrow.stroke()
text("拖到 Applications", 280, 237, 200, 28, 14, .medium, muted, centered: true)
color(245, 247, 248).setFill()
NSBezierPath(roundedRect: rect(32, 315, 696, 146), xRadius: 16, yRadius: 16).fill()
text("安装说明", 54, 332, 500, 24, 15, .semibold, ink)
let steps = ["将左侧应用拖到右侧 Applications", "从 Applications 打开 Tier0 Edge", "首次启动选择数据目录，等待初始化完成"]
for (index, step) in steps.enumerated() {
    let y = CGFloat(366 + index * 27)
    color(227, 232, 235).setFill()
    NSBezierPath(ovalIn: rect(54, y, 20, 20)).fill()
    text(String(index + 1), 54, y + 1, 20, 20, 12, .medium, ink, centered: true)
    text(step, 86, y - 1, 490, 26, 14, .regular, ink)
}
text("更新前请先退出并停止当前实例；替换应用不会删除数据。", 40, 477, 680, 20, 11, .regular, muted)
image.unlockFocus()
guard let tiff = image.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff),
      let png = bitmap.representation(using: .png, properties: [:]) else { fatalError("Cannot render DMG background") }
try png.write(to: output.appendingPathComponent("background.png"))

// Finder requires ICNS; every size is rendered from the existing web favicon.
guard let icon = NSImage(contentsOf: favicon) else { fatalError("Cannot read favicon") }
let iconset = output.appendingPathComponent("Tier0Edge.iconset", isDirectory: true)
try fm.createDirectory(at: iconset, withIntermediateDirectories: true)
for size in [16, 32, 128, 256, 512] {
    for scale in [1, 2] {
        let pixels = size * scale
        let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels,
                                  bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true,
                                  isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
        icon.draw(in: NSRect(x: 0, y: 0, width: pixels, height: pixels))
        NSGraphicsContext.restoreGraphicsState()
        let suffix = scale == 2 ? "@2x" : ""
        try rep.representation(using: .png, properties: [:])!.write(to: iconset.appendingPathComponent("icon_\(size)x\(size)\(suffix).png"))
    }
}
