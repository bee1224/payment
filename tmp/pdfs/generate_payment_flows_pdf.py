from pathlib import Path

from reportlab.lib import colors
from reportlab.lib.pagesizes import A4, landscape
from reportlab.lib.styles import ParagraphStyle, getSampleStyleSheet
from reportlab.lib.units import mm
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.platypus import Paragraph
from reportlab.pdfgen import canvas


ROOT = Path(__file__).resolve().parents[2]
OUT_DIR = ROOT / "payment-service" / "output" / "pdf"
OUT_DIR.mkdir(parents=True, exist_ok=True)
PDF_PATH = OUT_DIR / "代收代付流程圖.pdf"

FONT_PATH = Path(r"C:\Windows\Fonts\msjh.ttc")
FONT_NAME = "MSJH"


def register_font():
    if FONT_PATH.exists():
        pdfmetrics.registerFont(TTFont(FONT_NAME, str(FONT_PATH), subfontIndex=0))


def make_styles():
    styles = getSampleStyleSheet()
    return {
        "title": ParagraphStyle(
            "title",
            parent=styles["Title"],
            fontName=FONT_NAME,
            fontSize=22,
            leading=28,
            textColor=colors.HexColor("#0F172A"),
        ),
        "subtitle": ParagraphStyle(
            "subtitle",
            parent=styles["Heading2"],
            fontName=FONT_NAME,
            fontSize=13,
            leading=18,
            textColor=colors.HexColor("#334155"),
        ),
        "box": ParagraphStyle(
            "box",
            parent=styles["BodyText"],
            fontName=FONT_NAME,
            fontSize=11,
            leading=14,
            textColor=colors.HexColor("#0F172A"),
        ),
        "note": ParagraphStyle(
            "note",
            parent=styles["BodyText"],
            fontName=FONT_NAME,
            fontSize=10,
            leading=13,
            textColor=colors.HexColor("#475569"),
        ),
    }


def draw_paragraph(c, text, style, x, y, w, h):
    p = Paragraph(text.replace("\n", "<br/>"), style)
    p.wrapOn(c, w, h)
    p.drawOn(c, x, y)


def draw_box(c, x, y, w, h, title, body, styles, fill):
    c.setFillColor(fill)
    c.setStrokeColor(colors.HexColor("#CBD5E1"))
    c.roundRect(x, y, w, h, 8, fill=1, stroke=1)
    draw_paragraph(c, f"<b>{title}</b>", styles["subtitle"], x + 10, y + h - 28, w - 20, 24)
    draw_paragraph(c, body, styles["box"], x + 10, y + 12, w - 20, h - 42)


def draw_arrow(c, x1, y1, x2, y2, label, styles):
    c.setStrokeColor(colors.HexColor("#64748B"))
    c.setLineWidth(2)
    c.line(x1, y1, x2, y2)
    dx, dy = x2 - x1, y2 - y1
    size = 6
    if abs(dx) >= abs(dy):
        sign = 1 if dx >= 0 else -1
        c.line(x2, y2, x2 - sign * 10, y2 + size)
        c.line(x2, y2, x2 - sign * 10, y2 - size)
        lx, ly = (x1 + x2) / 2 - 20, y1 + 6
    else:
        sign = 1 if dy >= 0 else -1
        c.line(x2, y2, x2 - size, y2 - sign * 10)
        c.line(x2, y2, x2 + size, y2 - sign * 10)
        lx, ly = x1 + 6, (y1 + y2) / 2 + 4
    if label:
        draw_paragraph(c, label, styles["note"], lx, ly, 90, 20)


def page_header(c, title, subtitle, styles):
    width, height = landscape(A4)
    c.setFillColor(colors.HexColor("#F8FAFC"))
    c.rect(0, 0, width, height, fill=1, stroke=0)
    draw_paragraph(c, title, styles["title"], 18 * mm, height - 24 * mm, width - 40 * mm, 20 * mm)
    draw_paragraph(c, subtitle, styles["note"], 18 * mm, height - 33 * mm, width - 40 * mm, 10 * mm)


def draw_tech_deposit(c, styles):
    width, height = landscape(A4)
    page_header(c, "代收流程圖 - 工程師 / 技術版", "商戶建單 -> 本地建單 -> 玩家付款 -> 上游通知 -> 本地入帳 -> 通知商戶", styles)
    boxes = [
        ("1. 商戶建單", "POST /api/pay_order\n驗簽、時間戳、欄位檢查、渠道解析", 18, 120, 58, 34, "#DBEAFE"),
        ("2. 本地建立訂單", "建立 orders / provider_transactions\n回傳 order_id、view_url 等資訊", 86, 120, 58, 34, "#E0F2FE"),
        ("3. 玩家付款", "玩家前往支付頁\n完成刷卡 / 轉帳 / 超商付款", 154, 120, 58, 34, "#ECFCCB"),
        ("4. 上游通知我方", "Provider callback -> 驗證通知內容\n更新 provider_callbacks / 訂單狀態", 222, 120, 58, 34, "#FCE7F3"),
    ]
    for title, body, x, y, w, h, fill in boxes:
        draw_box(c, x * mm, y * mm, w * mm, h * mm, title, body, styles, colors.HexColor(fill))
    for i in range(len(boxes) - 1):
        draw_arrow(c, (18 + 58 + i * 68) * mm, 137 * mm, (86 + i * 68) * mm, 137 * mm, "", styles)
    draw_box(c, 52 * mm, 54 * mm, 72 * mm, 38 * mm, "5. 更新本地帳務", "更新 orders / provider_transactions / ledger_entries\n狀態改為成功或失敗", styles, colors.HexColor("#FEF3C7"))
    draw_box(c, 168 * mm, 54 * mm, 72 * mm, 38 * mm, "6. 通知商戶 / 商戶查單", "依 callback URL 通知商戶\n商戶也可 POST /api/query_transaction 查狀態", styles, colors.HexColor("#DCFCE7"))
    draw_arrow(c, 251 * mm, 120 * mm, 204 * mm, 92 * mm, "更新", styles)
    draw_arrow(c, 124 * mm, 73 * mm, 168 * mm, 73 * mm, "結果", styles)


def draw_tech_payout(c, styles):
    width, height = landscape(A4)
    page_header(c, "代付流程圖 - 工程師 / 技術版", "提款申請 -> 本地凍結 -> 審核 -> 送 TW4 -> callback / 補查 -> 通知商戶", styles)
    boxes = [
        ("1. 商戶申請提款", "POST /api/payouts\n驗證商戶、金額、銀行代碼白名單、餘額", 14, 118, 56, 36, "#DBEAFE"),
        ("2. 本地落單與凍結", "建立 payout_orders / payout_beneficiaries\n凍結商戶餘額，狀態 pending_review", 78, 118, 62, 36, "#E0F2FE"),
        ("3. 審核 / 拒絕 / 取消", "approve / reject / cancel\n需 X-Payout-Review-Token", 148, 118, 56, 36, "#FEF3C7"),
        ("4. 送 TW4", "approve 後才建立 payout_transactions\npay_notify_url 固定用 TW4_PAYOUT_NOTIFY_URL", 212, 118, 62, 36, "#ECFCCB"),
    ]
    for title, body, x, y, w, h, fill in boxes:
        draw_box(c, x * mm, y * mm, w * mm, h * mm, title, body, styles, colors.HexColor(fill))
    for i in range(len(boxes) - 1):
        left = boxes[i][2] + boxes[i][4]
        right = boxes[i + 1][2]
        draw_arrow(c, left * mm, 136 * mm, right * mm, 136 * mm, "", styles)
    draw_box(c, 46 * mm, 50 * mm, 76 * mm, 42 * mm, "5. TW4 callback / 補查", "POST /api/payments/callback 驗簽處理\n若通知遺失，由背景 reconcile 查單補齊", styles, colors.HexColor("#FCE7F3"))
    draw_box(c, 170 * mm, 50 * mm, 82 * mm, 42 * mm, "6. 本地入帳與通知商戶", "更新 completed / failed / reversed\n調整凍結餘額，建立 merchant callback task", styles, colors.HexColor("#DCFCE7"))
    draw_arrow(c, 244 * mm, 118 * mm, 206 * mm, 92 * mm, "狀態", styles)
    draw_arrow(c, 122 * mm, 71 * mm, 170 * mm, 71 * mm, "結果", styles)


def draw_pm_deposit(c, styles):
    page_header(c, "代收流程圖 - 商務 / PM 大綱版", "讓商戶知道收款單從建立到入帳的對外流程", styles)
    steps = [
        ("商戶送出收款訂單", "#DBEAFE"),
        ("系統建立代收單並回付款資訊", "#E0F2FE"),
        ("玩家完成付款", "#ECFCCB"),
        ("支付通道回傳付款結果", "#FCE7F3"),
        ("系統更新帳務與訂單狀態", "#FEF3C7"),
        ("通知商戶 / 商戶查單", "#DCFCE7"),
    ]
    x = 16
    for idx, (label, fill) in enumerate(steps, start=1):
        draw_box(c, x * mm, 92 * mm, 36 * mm, 28 * mm, f"{idx}", label, styles, colors.HexColor(fill))
        if idx < len(steps):
            draw_arrow(c, (x + 36) * mm, 106 * mm, (x + 41) * mm, 106 * mm, "", styles)
        x += 41
    draw_paragraph(c, "重點：商戶會拿到建單結果、付款結果通知，也可以主動查詢訂單狀態。", styles["note"], 18 * mm, 52 * mm, 240 * mm, 20 * mm)


def draw_pm_payout(c, styles):
    page_header(c, "代付流程圖 - 商務 / PM 大綱版", "讓商戶知道提款申請不是立即出款，而是經過審核與上游處理", styles)
    steps = [
        ("商戶送出提款申請", "#DBEAFE"),
        ("系統建立提款單並凍結額度", "#E0F2FE"),
        ("進入待審核", "#FEF3C7"),
        ("審核通過後送上游 TW4", "#ECFCCB"),
        ("TW4 回傳成功或失敗", "#FCE7F3"),
        ("系統更新狀態並通知商戶", "#DCFCE7"),
    ]
    x = 16
    for idx, (label, fill) in enumerate(steps, start=1):
        draw_box(c, x * mm, 92 * mm, 36 * mm, 28 * mm, f"{idx}", label, styles, colors.HexColor(fill))
        if idx < len(steps):
            draw_arrow(c, (x + 36) * mm, 106 * mm, (x + 41) * mm, 106 * mm, "", styles)
        x += 41
    draw_paragraph(c, "重點：提款單可查詢、可審核拒絕、可在未送上游前取消；已成功出款的訂單不受取消 API 影響。", styles["note"], 18 * mm, 48 * mm, 248 * mm, 24 * mm)


def main():
    register_font()
    styles = make_styles()
    c = canvas.Canvas(str(PDF_PATH), pagesize=landscape(A4))
    for draw in [draw_tech_deposit, draw_tech_payout, draw_pm_deposit, draw_pm_payout]:
        draw(c, styles)
        c.showPage()
    c.save()
    print(PDF_PATH)


if __name__ == "__main__":
    main()
