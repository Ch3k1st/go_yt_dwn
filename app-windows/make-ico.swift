// make-ico — программная отрисовка иконки VideoDownloader.exe.
// Запуск: swift app-windows/make-ico.swift app-windows/icon.ico
// Результат коммитится в репозиторий: сборка оболочки идёт с мака, но go:embed
// и rsrc должны работать без Swift под рукой (см. цель make windows-icon).
//
// Арт тот же, что у macOS-иконки (app/make-icon.swift): Flat Design по базе
// ui-ux-pro-max — простые формы, без градиентов и теней, палитра из токенов
// интерфейса (ui.go): подложка --primary #2563EB, стрелка белая,
// полка --warn #D97706. Отличия под Windows:
//   * поле меньше и радиус меньше (4 % и 16 % против macOS-squircle 10 %/22.4 %) —
//     на Windows иконки не обрезаются системной маской;
//   * на 16–24 px полка не рисуется: полоска в 1 пиксель превращается в грязь,
//     а стрелка сама по себе читается.

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
        colorSpaceName: .deviceRGB, bytesPerRow: px * 4, bitsPerPixel: 32)
    else { return nil }
    guard let ctx = NSGraphicsContext(bitmapImageRep: rep) else { return nil }
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = ctx

    let s = CGFloat(px)
    let margin = s * 0.04
    let plate = NSRect(x: margin, y: margin, width: s - margin * 2, height: s - margin * 2)
    palette.plate.setFill()
    NSBezierPath(roundedRect: plate, xRadius: plate.width * 0.16, yRadius: plate.width * 0.16).fill()

    let cx = s / 2
    let small = px <= 24

    if !small {
        let trayW = s * 0.44, trayH = s * 0.068
        palette.tray.setFill()
        NSBezierPath(roundedRect: NSRect(x: cx - trayW / 2, y: s * 0.20, width: trayW, height: trayH),
                     xRadius: trayH / 2, yRadius: trayH / 2).fill()
    }

    // Стрелка вниз: прямоугольная ножка + треугольная голова.
    palette.arrow.setFill()
    let stemW = s * (small ? 0.16 : 0.125)
    let stemBottom = small ? s * 0.46 : s * 0.50
    NSBezierPath(roundedRect: NSRect(x: cx - stemW / 2, y: stemBottom,
                                     width: stemW, height: s * (small ? 0.30 : 0.24)),
                 xRadius: stemW * 0.25, yRadius: stemW * 0.25).fill()

    let headW = s * (small ? 0.46 : 0.38)
    let headTop = small ? s * 0.50 : s * 0.535
    let headTip = small ? s * 0.20 : s * 0.325
    let head = NSBezierPath()
    head.move(to: NSPoint(x: cx - headW / 2, y: headTop))
    head.line(to: NSPoint(x: cx + headW / 2, y: headTop))
    head.line(to: NSPoint(x: cx, y: headTip))
    head.close()
    head.fill()

    NSGraphicsContext.restoreGraphicsState()
    return rep
}

/// Кадр .ico в формате DIB: BITMAPINFOHEADER + BGRA снизу вверх + пустая AND-маска
/// (прозрачность берётся из альфа-канала, как принято для 32-битных иконок).
func dibFrame(_ rep: NSBitmapImageRep) -> Data {
    let w = rep.pixelsWide, h = rep.pixelsHigh
    guard let src = rep.bitmapData else { return Data() }
    let stride = rep.bytesPerRow

    var out = Data()
    func u16(_ v: Int) { var x = UInt16(truncatingIfNeeded: v); withUnsafeBytes(of: &x) { out.append(contentsOf: $0) } }
    func u32(_ v: Int) { var x = UInt32(truncatingIfNeeded: v); withUnsafeBytes(of: &x) { out.append(contentsOf: $0) } }

    u32(40); u32(w); u32(h * 2); u16(1); u16(32)   // biSize..biBitCount
    u32(0); u32(w * h * 4); u32(0); u32(0); u32(0); u32(0) // сжатие, размер, разрешения, палитра

    for row in (0..<h).reversed() {
        for col in 0..<w {
            let p = src + row * stride + col * 4
            out.append(p[2]); out.append(p[1]); out.append(p[0]); out.append(p[3]) // RGBA -> BGRA
        }
    }
    let maskRow = ((w + 31) / 32) * 4
    out.append(Data(count: maskRow * h))
    return out
}

let sizes: [(px: Int, png: Bool)] = [
    (16, false), (20, false), (24, false), (32, false), (48, false), (64, false),
    (128, true), (256, true),
]

guard CommandLine.arguments.count == 2 else {
    FileHandle.standardError.write(Data("использование: swift make-ico.swift <файл.ico>\n".utf8))
    exit(2)
}

var frames: [(px: Int, data: Data)] = []
for size in sizes {
    guard let rep = drawIcon(px: size.px) else {
        FileHandle.standardError.write(Data("не удалось отрисовать \(size.px)px\n".utf8))
        exit(1)
    }
    let data: Data? = size.png ? rep.representation(using: .png, properties: [:]) : dibFrame(rep)
    guard let data, !data.isEmpty else {
        FileHandle.standardError.write(Data("пустой кадр \(size.px)px\n".utf8))
        exit(1)
    }
    frames.append((size.px, data))
}

// ICONDIR (6 байт) + ICONDIRENTRY по 16 байт + кадры подряд.
var ico = Data()
func u16(_ v: Int) { var x = UInt16(truncatingIfNeeded: v); withUnsafeBytes(of: &x) { ico.append(contentsOf: $0) } }
func u32(_ v: Int) { var x = UInt32(truncatingIfNeeded: v); withUnsafeBytes(of: &x) { ico.append(contentsOf: $0) } }

u16(0); u16(1); u16(frames.count)
var offset = 6 + 16 * frames.count
for f in frames {
    ico.append(UInt8(f.px == 256 ? 0 : f.px))   // 0 означает 256
    ico.append(UInt8(f.px == 256 ? 0 : f.px))
    ico.append(0); ico.append(0)                // палитра, резерв
    u16(1); u16(32)                             // планы, бит на пиксель
    u32(f.data.count); u32(offset)
    offset += f.data.count
}
for f in frames { ico.append(f.data) }

try ico.write(to: URL(fileURLWithPath: CommandLine.arguments[1]))
