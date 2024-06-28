#!/usr/bin/env python3

# Docs:
#     This script uses credentials (username and password) from users.json
#     to login to console.dev.redhat.com and generate new offline token. It
#     saves updated content to users-new.json.
#
# Setup:
#     python -m venv venv
#     source venv/bin/activate
#     pip install playwright
#     playwright install   # download Chromium browser
#
# Running:
#     Consider PLAYWRIGHT_HEADLESS = True so it runs in background
#
#     ci-scripts/utility_scripts/playwright-update-tokens.py
#
#     To sort final file I have used:
#     cat users-new.json | jq '. | sort_by(.username[11:] | tonumber)'
#
#     Note I was not able to run with more threads as I was getting
#     "Access Denied" errors. I guess it was some rate limiting.

import playwright.sync_api
import os
import time
import json
import concurrent.futures

PLAYWRIGHT_HEADLESS = False
PLAYWRIGHT_VIDEO_DIR = "videos/"

def thread(user):
    username = user["username"].replace("-", "_")
    password = user["password"]

    with playwright.sync_api.sync_playwright() as p:
        browser = p.chromium.launch(
            headless=PLAYWRIGHT_HEADLESS,
        )
        context = browser.new_context(
            record_video_dir=PLAYWRIGHT_VIDEO_DIR,
        )
        page = context.new_page()

        page.goto("https://console.dev.redhat.com")
        page.wait_for_url("https://sso.redhat.com/**")

        # Accept cookies
        cookies_iframe = page.frame_locator('iframe[name="trustarc_cm"]')
        cookies_button = cookies_iframe.get_by_role("button", name="Agree and proceed with standard settings")
        cookies_button.click()

        # Wait for login form and use it
        page.wait_for_selector('//h1[text()="Log in to your Red Hat account"]')
        input_user = page.locator('//input[@id="username-verification"]')
        time.sleep(1)
        input_user.fill(username)
        button_next = page.locator('//button[@id="login-show-step2"]')
        button_next.click()
        button_next.wait_for(state="hidden")
        input_pass = page.locator('//input[@id="password"]')
        input_pass.wait_for(state="visible")
        input_pass.fill(password)
        page.locator('//button[@id="rh-password-verification-submit-button"]').click()

        # Wait for console to load and go to OpenShift Token page
        page.wait_for_url("https://console.dev.redhat.com/**")
        page.wait_for_selector('//h2[text()="Welcome to your Hybrid Cloud Console."]')
        page.goto("https://console.dev.redhat.com/openshift/token")
        page.wait_for_url("https://console.dev.redhat.com/openshift/token**")
        page.wait_for_selector('//h2[text()="Connect with offline tokens"]')

        # Wait for token 
        button_token = page.locator('//button[text()="Load token"]')
        if button_token.is_visible():
            button_token.click()
        while True:
            input_token = page.locator('//input[@aria-label="Copyable token" and not(contains(@value, "ocm login "))]')
            input_token_value = input_token.get_attribute("value")
            if len(input_token_value) > 10:
                break
        print(f"Token for user {username}: {input_token_value}")

        page.close()
        browser.close()

        user["token"] = input_token_value
        return user

def main():

    with open("users.json", "r") as fd:
        users = json.load(fd)

    users_new = []
    users_allowlist = []   # keep empty to allow all
    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor:
        futures = {executor.submit(thread, user): user["username"] for user in users if users_allowlist != [] and user["username"] in users_allowlist}
        for future in concurrent.futures.as_completed(futures):
            username = futures[future]
            try:
                user = future.result()
            except Exception as exc:
                print(f"Failed processing {username}: {exc}")
            else:
                print(f"Finished processing {username}")
                users_new.append(user)

    with open("users-new.json", "w") as fd:
        print(f"Dumping {len(users_new)} users")
        users = json.dump(users_new, fd)

if __name__ == "__main__":
    main()
