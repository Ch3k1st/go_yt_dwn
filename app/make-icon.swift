// make-icon — программная отрисовка иконки Video Downloader.app.
// Запуск: swift app/make-icon.swift <выходной .iconset>; дальше iconutil.
//
// Дизайн по базе ui-ux-pro-max (Flat Design): простые геометрические формы,
// без градиентов и теней, цвета — те же токены, что и в интерфейсе (ui.go):
// подложка folder blue #2563EB, стрелка белая, полка-«лоток» amber #D97706.
// Треугольник вниз читается сразу и как «play» (это видео), и как «скачать».
// Формы крупные: силуэт не разваливается на 16px.

import AppKit

let palette = (
    plate: NSColor(srgbRed: 0x25 / 255, green: 0x63 / 255, blue: 0xEB / 255, alpha: 1), // --primary
    arrow: NSColor.white,
    tray: NSColor(srgbRed: 0xD9 / 255, green: 0x77 / 255, blue: 0x06 / 255, alpha: 1)   // --warn
)

func drawIcon(px: Int) -> NSBitmapImageRep? {
    guard let rep = NSBitmapImageRep(
        bitmapDataPlanes: nil, pixelsWide: px, pixelsHigh: px, bitsPerSample: 8,
        samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
        colorSpaceName: .calibratedRGB, bytesPerRow: 0, bitsPerPixel: 0)
    else { return nil }
    guard let ctx = NSGraphicsContext(bitmapImageRep: rep) else { return nil }
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = ctx

    let s = CGFloat(px)
    // Скруглённый квадрат в пропорциях системных иконок macOS:
    // поле ~10% с каждой стороны, радиус ~22.4% от стороны фигуры.
    let margin = s * 0.098
    let plate = NSRect(x: margin, y: margin, width: s - margin * 2, height: s - margin * 2)
    palette.plate.setFill()
    NSBezierPath(roundedRect: plate, xRadius: plate.width * 0.224, yRadius: plate.width * 0.224).fill()

    let cx = s / 2
    // Полка внизу — «куда падает файл».
    let trayW = s * 0.40, trayH = s * 0.062
    palette.tray.setFill()
    NSBezierPath(roundedRect: NSRect(x: cx - trayW / 2, y: s * 0.255, width: trayW, height: trayH),
                 xRadius: trayH / 2, yRadius: trayH / 2).fill()

    // Стрелка вниз: прямоугольная ножка + треугольная голова.
    palette.arrow.setFill()
    let stemW = s * 0.115
    NSBezierPath(roundedRect: NSRect(x: cx - stemW / 2, y: s * 0.53, width: stemW, height: s * 0.20),
                 xRadius: stemW * 0.28, yRadius: stemW * 0.28).fill()

    let headW = s * 0.34, headTop = s * 0.565, headTip = s * 0.375
    let head = NSBezierPath()
    head.move(to: NSPoint(x: cx - headW / 2, y: headTop))
    head.line(to: NSPoint(x: cx + headW / 2, y: headTop))
    head.line(to: NSPoint(x: cx, y: headTip))
    head.close()
    head.fill()

    NSGraphicsContext.restoreGraphicsState()
    return rep
}

// icon_NxN[@2x].png — набор, который понимает iconutil.
let variants: [(name: String, px: Int)] = [
    ("icon_16x16", 16), ("icon_16x16@2x", 32),
    ("icon_32x32", 32), ("icon_32x32@2x", 64),
    ("icon_128x128", 128), ("icon_128x128@2x", 256),
    ("icon_256x256", 256), ("icon_256x256@2x", 512),
    ("icon_512x512", 512), ("icon_512x512@2x", 1024),
]

guard CommandLine.arguments.count == 2 else {
    FileHandle.standardError.write(Data("использование: swift make-icon.swift <dir.iconset>\n".utf8))
    exit(2)
}
let outDir = CommandLine.arguments[1]
try FileManager.default.createDirectory(atPath: outDir, withIntermediateDirectories: true)
for v in variants {
    guard let rep = drawIcon(px: v.px), let png = rep.representation(using: .png, properties: [:]) else {
        FileHandle.standardError.write(Data("не удалось отрисовать \(v.name)\n".utf8))
        exit(1)
    }
    try png.write(to: URL(fileURLWithPath: outDir).appendingPathComponent("\(v.name).png"))
}
