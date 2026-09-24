#!/usr/bin/env swift

import AppKit
import Foundation

// Finder places the app and Applications at about x=151 and x=388 in a
// 540 × 380 DMG window. Keep the middle row clear for those real icons.
let width = 540
let height = 380
let scale = 2 // Retina: 1080 × 760 pixels at the same 540 × 380 point size.
let output = CommandLine.arguments.dropFirst().first ?? "build/darwin/dmg-background.png"

func color(_ red: CGFloat, _ green: CGFloat, _ blue: CGFloat, _ alpha: CGFloat = 1) -> NSColor {
    NSColor(calibratedRed: red / 255, green: green / 255, blue: blue / 255, alpha: alpha)
}

func rect(_ x: CGFloat, _ top: CGFloat, _ rectWidth: CGFloat, _ rectHeight: CGFloat) -> NSRect {
    NSRect(x: x, y: CGFloat(height) - top - rectHeight, width: rectWidth, height: rectHeight)
}

func centeredText(_ text: String, top: CGFloat, size: CGFloat, weight: NSFont.Weight, ink: NSColor) {
    let style = NSMutableParagraphStyle()
    style.alignment = .center
    (text as NSString).draw(
        in: rect(20, top, 500, size + 9),
        withAttributes: [
            .font: NSFont.systemFont(ofSize: size, weight: weight),
            .foregroundColor: ink,
            .paragraphStyle: style,
        ]
    )
}

guard let bitmap = NSBitmapImageRep(
    bitmapDataPlanes: nil,
    pixelsWide: width * scale,
    pixelsHigh: height * scale,
    bitsPerSample: 8,
    samplesPerPixel: 4,
    hasAlpha: true,
    isPlanar: false,
    colorSpaceName: .deviceRGB,
    bytesPerRow: 0,
    bitsPerPixel: 0
), let context = NSGraphicsContext(bitmapImageRep: bitmap) else {
    fatalError("Could not create DMG background canvas")
}

NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = context
context.imageInterpolation = .high
context.cgContext.scaleBy(x: CGFloat(scale), y: CGFloat(scale))
bitmap.size = NSSize(width: width, height: height)

let background = NSGradient(starting: color(255, 252, 250), ending: color(247, 249, 252))!
background.draw(in: rect(0, 0, 540, 380), angle: -90)

let accent = NSBezierPath(roundedRect: rect(254, 34, 32, 4), xRadius: 2, yRadius: 2)
color(207, 110, 98).setFill()
accent.fill()

centeredText("Install Vela", top: 55, size: 22, weight: .semibold, ink: color(43, 48, 57))
centeredText("Drag Vela into Applications", top: 88, size: 12, weight: .regular, ink: color(109, 116, 126))

let arrowDisc = NSBezierPath(ovalIn: rect(248, 152, 44, 44))
color(255, 239, 235).setFill()
arrowDisc.fill()
color(242, 215, 208).setStroke()
arrowDisc.lineWidth = 1
arrowDisc.stroke()

let arrow = NSBezierPath()
arrow.lineCapStyle = .round
arrow.lineJoinStyle = .round
arrow.lineWidth = 2.5
arrow.move(to: NSPoint(x: 259, y: 206))
arrow.line(to: NSPoint(x: 280, y: 206))
arrow.move(to: NSPoint(x: 273, y: 213))
arrow.line(to: NSPoint(x: 280, y: 206))
arrow.line(to: NSPoint(x: 273, y: 199))
color(195, 94, 82).setStroke()
arrow.stroke()

context.flushGraphics()
NSGraphicsContext.restoreGraphicsState()

guard let png = bitmap.representation(using: .png, properties: [:]) else {
    fatalError("Could not encode DMG background")
}
try png.write(to: URL(fileURLWithPath: output))
