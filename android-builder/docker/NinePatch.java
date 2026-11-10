import javax.imageio.ImageIO;
import java.awt.Color;
import java.awt.Graphics2D;
import java.awt.image.BufferedImage;
import java.io.File;

/**
 * 把普通 png 转成 Android .9.png（nine-patch）启动图。
 *
 * 原理：DCloud 运行时绘制启动图前会检查 Bitmap.getNinePatchChunk()，
 * 是 nine-patch 就用 NinePatchDrawable 绘制 —— 拉伸只发生在黑线标记的区域，
 * 内容区（如居中的 logo）保持原样，从而适配任意屏幕不变形。
 *
 * 拉伸段标记规则：
 *   top / left 边：在「内容包围盒」两侧标记拉伸段，内容列/行被钉在中间
 *   bottom / right 边：整边标记 → 内容区 = 整图（内容不被 padding 内缩）
 *
 * 用法: java -cp /opt/pack NinePatch <in.png> <out.9.png>
 */
public class NinePatch {

    private static final int BLACK = 0xFF000000;

    public static void main(String[] args) throws Exception {
        if (args.length < 2) {
            System.err.println("usage: NinePatch <in.png> <out.png>");
            System.exit(2);
        }
        BufferedImage src = ImageIO.read(new File(args[0]));
        if (src == null) {
            System.err.println("无法读取图片: " + args[0]);
            System.exit(3);
        }
        int w = src.getWidth(), h = src.getHeight();
        Color bg = cornerColor(src);

        // 1) 找内容包围盒（与背景色差异明显的像素）
        int bx0 = w, by0 = h, bx1 = -1, by1 = -1;
        for (int y = 0; y < h; y++) {
            for (int x = 0; x < w; x++) {
                if (isContent(src.getRGB(x, y), bg)) {
                    if (x < bx0) bx0 = x;
                    if (x > bx1) bx1 = x;
                    if (y < by0) by0 = y;
                    if (y > by1) by1 = y;
                }
            }
        }

        boolean fullStretch = bx1 < 0; // 整图纯色，随便拉
        if (!fullStretch) {
            int m = Math.max(4, Math.min(w, h) / 40);
            bx0 = Math.max(0, bx0 - m);
            by0 = Math.max(0, by0 - m);
            bx1 = Math.min(w - 1, bx1 + m);
            by1 = Math.min(h - 1, by1 + m);
            // 内容占满整图（全出血设计）→ 只能整图均匀拉伸
            if ((long) (bx1 - bx0 + 1) * (by1 - by0 + 1) > (long) w * h * 9 / 10) {
                fullStretch = true;
            }
        }

        // 2) 四周扩 1px 透明边框，源图放在 (1,1)
        int W = w + 2, H = h + 2;
        BufferedImage out = new BufferedImage(W, H, BufferedImage.TYPE_INT_ARGB);
        Graphics2D g = out.createGraphics();
        g.drawImage(src, 1, 1, null);
        g.dispose();

        // 3) 画黑线标记（四角必须透明）
        if (fullStretch) {
            for (int x = 1; x <= w; x++) out.setRGB(x, 0, BLACK);
            for (int y = 1; y <= h; y++) out.setRGB(0, y, BLACK);
        } else {
            // top：内容列两侧为水平拉伸段
            for (int x = 1; x <= bx0; x++) out.setRGB(x, 0, BLACK);
            for (int x = bx1 + 2; x <= w; x++) out.setRGB(x, 0, BLACK);
            // left：内容行两侧为垂直拉伸段
            for (int y = 1; y <= by0; y++) out.setRGB(0, y, BLACK);
            for (int y = by1 + 2; y <= h; y++) out.setRGB(0, y, BLACK);
        }
        // bottom / right：整边 → 内容区为整图
        for (int x = 1; x <= w; x++) out.setRGB(x, H - 1, BLACK);
        for (int y = 1; y <= h; y++) out.setRGB(W - 1, y, BLACK);

        ImageIO.write(out, "png", new File(args[1]));
        System.out.println("ninepatch " + W + "x" + H
                + (fullStretch ? " full-stretch" : " content-locked")
                + " bg=#" + String.format("%06X", bg.getRGB() & 0xFFFFFF));
    }

    /** 背景色：四个角的不透明像素均值，取不到用白色 */
    private static Color cornerColor(BufferedImage img) {
        int w = img.getWidth(), h = img.getHeight();
        int[][] pts = {{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}};
        long r = 0, g = 0, b = 0;
        int n = 0;
        for (int[] p : pts) {
            int c = img.getRGB(p[0], p[1]);
            if (((c >>> 24) & 0xFF) > 200) {
                r += (c >> 16) & 0xFF;
                g += (c >> 8) & 0xFF;
                b += c & 0xFF;
                n++;
            }
        }
        if (n == 0) return Color.WHITE;
        return new Color((int) (r / n), (int) (g / n), (int) (b / n));
    }

    private static boolean isContent(int c, Color bg) {
        int alpha = (c >>> 24) & 0xFF;
        if (alpha < 40) return false; // 透明 = 背景
        int dr = Math.abs(((c >> 16) & 0xFF) - bg.getRed());
        int dg = Math.abs(((c >> 8) & 0xFF) - bg.getGreen());
        int db = Math.abs((c & 0xFF) - bg.getBlue());
        return dr + dg + db > 60;
    }
}
