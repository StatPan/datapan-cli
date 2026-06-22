from __future__ import annotations

import argparse
import asyncio
import json
import sys
from pathlib import Path

DATA_GO_KR_BASE_URL = "https://www.data.go.kr"
DATA_GO_KR_LOGIN_URL = f"{DATA_GO_KR_BASE_URL}/uim/login/loginView.do"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)

    login = subparsers.add_parser("login")
    login.add_argument("--storage-state", required=True)
    login.add_argument("--headed", action="store_true")
    login.add_argument("--manual-login-wait-ms", type=int, default=120000)

    submit = subparsers.add_parser("submit")
    submit.add_argument("--list-id", required=True)
    submit.add_argument("--application-url", required=True)
    submit.add_argument("--storage-state", required=True)
    submit.add_argument("--purpose-text", required=True)
    submit.add_argument("--apply", action="store_true")
    submit.add_argument("--output")
    return parser.parse_args()


def print_json(payload: dict) -> None:
    print(json.dumps(payload, ensure_ascii=False, indent=2))


async def main_async(args: argparse.Namespace) -> int:
    try:
        from playwright.async_api import async_playwright
    except ModuleNotFoundError:
        print_json(
            {
                "ok": False,
                "status": "playwright_missing",
                "message": "Install Playwright first: python -m pip install playwright && python -m playwright install chromium",
            }
        )
        return 4

    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=not getattr(args, "headed", False))
        try:
            if args.command == "login":
                payload = await run_login(browser, args)
            elif args.command == "submit":
                payload = await run_submit(browser, args)
            else:
                raise AssertionError(args.command)
        finally:
            await browser.close()

    if getattr(args, "output", None):
        output = Path(args.output)
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
    print_json(payload)
    return 0 if payload.get("ok") else 4


async def run_login(browser, args: argparse.Namespace) -> dict:
    context = await browser.new_context(locale="ko-KR")
    page = await context.new_page()
    await page.goto(DATA_GO_KR_LOGIN_URL, wait_until="domcontentloaded")
    await page.wait_for_timeout(1000)

    before = await page.locator("body").inner_text(timeout=10000)
    has_gate = has_human_gate(before)
    if args.manual_login_wait_ms > 0:
        await page.wait_for_timeout(args.manual_login_wait_ms)

    body = await page.locator("body").inner_text(timeout=10000)
    confirmed = is_login_confirmed(page.url, body)
    if confirmed:
        state = Path(args.storage_state)
        state.parent.mkdir(parents=True, exist_ok=True)
        await context.storage_state(path=str(state))
    return {
        "ok": confirmed,
        "command": "login",
        "provider": "data.go.kr",
        "status": "session_saved" if confirmed else "manual_login_timeout",
        "storage_state": args.storage_state if confirmed else "",
        "login_confirmed": confirmed,
        "human_gate_detected": has_gate or has_human_gate(body),
        "url": page.url,
    }


async def run_submit(browser, args: argparse.Namespace) -> dict:
    state = Path(args.storage_state)
    if not state.exists():
        return {
            "ok": False,
            "command": "submit",
            "provider": "data.go.kr",
            "status": "storage_state_missing",
            "storage_state": args.storage_state,
        }

    context = await browser.new_context(locale="ko-KR", storage_state=str(state))
    page = await context.new_page()
    await page.goto(DATA_GO_KR_BASE_URL, wait_until="domcontentloaded")
    await page.wait_for_timeout(1000)
    body = await page.locator("body").inner_text(timeout=10000)
    if not is_login_confirmed(page.url, body):
        return {
            "ok": False,
            "command": "submit",
            "provider": "data.go.kr",
            "status": "session_expired_or_login_required",
            "login_confirmed": False,
            "url": page.url,
            "human_gate_detected": has_human_gate(body),
        }

    await page.goto(args.application_url, wait_until="domcontentloaded")
    await page.wait_for_timeout(1000)
    page_text = await page.locator("body").inner_text(timeout=10000)
    detected = detect_application_state(page_text)
    result = {
        "ok": True,
        "command": "submit",
        "provider": "data.go.kr",
        "list_id": args.list_id,
        "application_url": args.application_url,
        "login_confirmed": True,
        "dry_run": not args.apply,
        "detected_state": detected,
        "action": "dry_run_inspection",
    }
    if not args.apply:
        return result
    if detected["status"] != "access_user_action_required":
        result["action"] = "not_submitted"
        return result
    apply_result = await try_submit_application(page, args.purpose_text)
    result["action"] = apply_result["action"]
    result["apply_result"] = apply_result
    return result


def detect_application_state(page_text: str) -> dict:
    markers = {
        "has_apply_text": "활용신청" in page_text,
        "has_cancel_text": "신청취소" in page_text,
        "has_approved_text": "승인" in page_text,
        "has_login_text": "로그인" in page_text,
        "human_gate_detected": has_human_gate(page_text),
    }
    if markers["human_gate_detected"]:
        status = "human_gate"
    elif markers["has_cancel_text"]:
        status = "access_requested_not_confirmed"
    elif markers["has_apply_text"] and markers["has_approved_text"]:
        status = "ambiguous_manual_review"
    elif markers["has_approved_text"]:
        status = "access_requested_not_confirmed"
    elif markers["has_apply_text"]:
        status = "access_user_action_required"
    elif markers["has_login_text"]:
        status = "not_logged_in_or_session_expired"
    else:
        status = "unknown"
    return {"status": status, **markers}


async def try_submit_application(page, purpose_text: str) -> dict:
    selector = await first_visible_selector(
        page,
        ["button:has-text('활용신청')", "a:has-text('활용신청')", "input[value='활용신청']"],
    )
    if not selector:
        return {"action": "apply_control_not_found"}
    await page.click(selector)
    try:
        await page.wait_for_load_state("networkidle", timeout=10000)
    except Exception:
        pass
    page_text = await page.locator("body").inner_text(timeout=10000)
    if has_human_gate(page_text):
        return {"action": "access_user_action_required"}
    if looks_requested_or_granted(page_text):
        return {"action": "access_requested_not_confirmed"}

    filled = await fill_application_form(page, purpose_text)
    submit_selector = await first_visible_selector(
        page,
        [
            "button:has-text('신청')",
            "input[value='신청']",
            "button:has-text('등록')",
            "input[value='등록']",
            "button:has-text('저장')",
            "input[value='저장']",
            "button:has-text('확인')",
            "input[value='확인']",
        ],
    )
    if not submit_selector:
        return {"action": "apply_form_submit_control_not_found", "filled": filled}
    page.once("dialog", lambda dialog: asyncio.create_task(dialog.accept()))
    await page.click(submit_selector)
    try:
        await page.wait_for_load_state("networkidle", timeout=10000)
    except Exception:
        pass
    await page.wait_for_timeout(1000)
    final_text = await page.locator("body").inner_text(timeout=10000)
    return {"action": classify_apply_result(final_text), "filled": filled}


async def fill_application_form(page, purpose_text: str) -> dict[str, int]:
    filled = {"textarea": 0, "text_input": 0, "checkbox": 0, "select": 0}
    textareas = page.locator("textarea:visible")
    for index in range(await textareas.count()):
        locator = textareas.nth(index)
        try:
            if not (await locator.input_value(timeout=1000)).strip():
                await locator.fill(purpose_text)
                filled["textarea"] += 1
        except Exception:
            continue
    inputs = page.locator("input:visible")
    for index in range(await inputs.count()):
        locator = inputs.nth(index)
        try:
            input_type = (await locator.get_attribute("type") or "text").lower()
            if input_type not in {"text", "search"}:
                continue
            label = " ".join(
                await locator_attr(locator, attr)
                for attr in ("name", "id", "placeholder", "title")
            ).lower()
            if not is_purpose_field(label):
                continue
            if not (await locator.input_value(timeout=1000)).strip():
                await locator.fill(purpose_text)
                filled["text_input"] += 1
        except Exception:
            continue
    checkboxes = page.locator("input[type='checkbox']:visible")
    for index in range(await checkboxes.count()):
        locator = checkboxes.nth(index)
        try:
            if not await locator.is_checked():
                await locator.check()
                filled["checkbox"] += 1
        except Exception:
            continue
    selects = page.locator("select:visible")
    for index in range(await selects.count()):
        locator = selects.nth(index)
        try:
            if str(await locator.input_value(timeout=1000)).strip():
                continue
            options = locator.locator("option")
            for option_index in range(await options.count()):
                option = options.nth(option_index)
                value = (await option.get_attribute("value") or "").strip()
                if value:
                    await locator.select_option(value=value)
                    filled["select"] += 1
                    break
        except Exception:
            continue
    return filled


async def first_visible_selector(page, selectors: list[str]) -> str | None:
    for selector in selectors:
        try:
            locator = page.locator(selector).first
            if await locator.count() and await locator.is_visible(timeout=1500):
                return selector
        except Exception:
            continue
    return None


async def locator_attr(locator, attr: str) -> str:
    try:
        return await locator.get_attribute(attr) or ""
    except Exception:
        return ""


def is_purpose_field(label: str) -> bool:
    return any(
        term in label
        for term in ("활용", "목적", "사용", "내용", "사유", "비고", "설명", "purpose", "reason", "use", "usage", "cont")
    )


def classify_apply_result(page_text: str) -> str:
    if has_human_gate(page_text):
        return "access_user_action_required"
    if any(term in page_text for term in ("신청완료", "신청되었습니다", "승인대기")):
        return "access_requested_not_confirmed"
    if looks_requested_or_granted(page_text):
        return "access_requested_not_confirmed"
    if "필수" in page_text or "입력" in page_text:
        return "access_user_action_required"
    return "apply_submitted_review_required"


def looks_requested_or_granted(page_text: str) -> bool:
    return any(term in page_text for term in ("신청취소", "이미 신청", "승인", "활용중", "사용중"))


def is_login_confirmed(url: str, page_text: str) -> bool:
    if "auth.data.go.kr" in url:
        return False
    return any(term in page_text for term in ("로그아웃", "마이페이지", "My Page"))


def has_human_gate(page_text: str) -> bool:
    return any(
        term in page_text
        for term in ("보안문자", "자동입력", "본인인증", "휴대폰 인증", "아이핀", "공동인증서", "captcha", "CAPTCHA")
    )


def main() -> int:
    return asyncio.run(main_async(parse_args()))


if __name__ == "__main__":
    raise SystemExit(main())
