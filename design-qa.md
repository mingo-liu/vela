# Vela 页面设计核对

- Source visual truth: `/var/folders/xv/jx6nvb4s2pg1cqvg7xkg47vc0000gn/T/29A1FA17-0CB6-4876-BBEA-80E496C36B30-484-000006ED49722B37/pasted-image-20260923-190704.png`
- Implementation screenshot: `/private/tmp/vela-design-qa-home-final.png`
- Side-by-side comparison: `/private/tmp/vela-design-qa-comparison-1x.png`
- Viewport: source 1450 × 770 px; Vela expanded native window 1532 × 768 px. The comparison keeps both captures at their original pixel size, with a 2 px vertical offset for alignment. The source has no browser chrome; the Vela capture includes the macOS title bar.
- State: source is a library search screen; Vela is Home with the system proxy off and a profile imported. The reference supplies the shell's visual language, while Vela's content follows the requested Home, Proxies, Profiles modules.

## Findings

No actionable P0, P1, or P2 differences remain for the requested visual direction.

- Typography: the macOS system font gives the navigation the same clear, dark hierarchy as the reference. Smaller explanatory text uses a darker gray for legibility.
- Layout and spacing: the pale sidebar occupies about 26% of the expanded Vela window, close to the reference's 27%; the content area retains ample white space. Home modules fit without horizontal overflow.
- Color: pale gray navigation background, red outline icons, dark headings, and white content follow the reference. Green is reserved for connection state.
- Images and icons: the reference uses standard interface icons rather than raster content. Phosphor outline icons provide the corresponding Home, Proxies, and Profiles symbols; there are no missing image assets.
- Copy and content: the reference's library content is intentionally replaced with Vela connection controls and modules. The only system proxy switch is on Home.
- Focused comparison: the navigation region and Home control card were inspected in the combined image at native size. The source's selected navigation pill, icon color, sidebar edge, and main background are reflected in Vela. Exact card content has no counterpart in the reference.

## Comparison history

1. Initial expanded capture showed a fixed 246 px sidebar that was too narrow against the reference. Changed the sidebar to `clamp(225px, 27vw, 500px)`; the final expanded capture shows a matching major-region proportion.
2. Small secondary labels had low contrast. Darkened them to `#62666d`; the final capture shows readable supporting text on white and pale gray surfaces.
3. The YAML picker was mouse-only. Replaced its label control with a real button; the final accessibility tree exposes `选择 YAML 文件` as a button.

## Interaction check

In the packaged macOS app, Home, Proxies, and Profiles navigation opened their respective modules. The Proxies empty state and Profiles import controls appeared as expected. The YAML button opened the native file picker, which was cancelled without importing anything. The system proxy was not switched during visual QA.

## Follow-up polish

- P3: the reference's icon shapes differ slightly from the Phosphor equivalents. This does not affect recognition or navigation.

final result: passed
