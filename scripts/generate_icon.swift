import AppKit
import Foundation

guard CommandLine.arguments.count == 4,
      let size = Int(CommandLine.arguments[3]), size > 0, size <= 1024 else {
    fputs("usage: swift scripts/generate_icon.swift source.png output.png size(1...1024)\n", stderr)
    exit(2)
}

let source = URL(fileURLWithPath: CommandLine.arguments[1]).standardizedFileURL
let output = URL(fileURLWithPath: CommandLine.arguments[2]).standardizedFileURL
guard source != output, let image = NSImage(contentsOf: source), image.size.width == image.size.height,
let bitmap = NSBitmapImageRep(
    bitmapDataPlanes: nil,
    pixelsWide: size,
    pixelsHigh: size,
    bitsPerSample: 8,
    samplesPerPixel: 4,
    hasAlpha: true,
    isPlanar: false,
    colorSpaceName: .deviceRGB,
    bytesPerRow: 0,
    bitsPerPixel: 0
), let context = NSGraphicsContext(bitmapImageRep: bitmap) else {
    fatalError("source must be a square image; output must differ from source")
}

NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = context
context.imageInterpolation = .high
image.draw(in: NSRect(x: 0, y: 0, width: size, height: size))

NSGraphicsContext.restoreGraphicsState()

guard let data = bitmap.representation(using: .png, properties: [:]) else {
    fatalError("unable to encode icon PNG")
}
try FileManager.default.createDirectory(at: output.deletingLastPathComponent(), withIntermediateDirectories: true)
try data.write(to: output, options: .atomic)
