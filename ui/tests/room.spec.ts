import { test, expect } from "@playwright/test";

for (const size of [
  {
    name: "desktop",
    width: 1440,
    height: 1100,
  },
  {
    name: "mobile",
    width: 390,
    height: 844,
  },
]) {
  test(`${size.name}: password, media details and responsive timeline`, async ({
    page,
  }) => {
    await page.setViewportSize(size);
    await page.goto("/");
    await page.getByLabel("Your name").fill("Alice");
    await page.getByLabel("Room password").fill("wrong");
    await page.getByRole("button", { name: "Enter room" }).click();
    await expect(page.getByText("Incorrect password")).toBeVisible();
    await page.getByLabel("Room password").fill("testpass");
    await page.getByRole("button", { name: "Enter room" }).click();
    await expect(page.getByText("Room connected")).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Original media" }),
    ).toBeVisible();
    await expect(page.getByLabel("Movie position")).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBeTruthy();
    await page.screenshot({
      path: `/private/tmp/watchd-fixtures/${size.name}.png`,
      fullPage: true,
    });
    await page.getByRole("tab", { name: "Transcode", exact: true }).click();
    await expect(
      page.getByRole("button", { name: /Start transcoding|Replace transcode/ }),
    ).toBeVisible();
    await page.screenshot({
      path: `/private/tmp/watchd-fixtures/${size.name}-transcode.png`,
      fullPage: true,
    });
  });
}

test("two viewers synchronize pause, seek and shared transcoded playback", async ({
  browser,
}) => {
  const contexts = await Promise.all([
    browser.newContext(),
    browser.newContext(),
  ]);
  const pages = await Promise.all(contexts.map((context) => context.newPage()));
  const errors: string[] = [];
  for (const [index, page] of pages.entries()) {
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto("/");
    await page.getByLabel("Your name").fill(index ? "Bob" : "Alice");
    await page.getByLabel("Room password").fill("testpass");
    await page.getByRole("button", { name: "Enter room" }).click();
    await expect(page.getByText("Room connected")).toBeVisible();
    await page.getByRole("button", { name: "Source", exact: true }).click();
    await page.getByRole("button", { name: "Join & play" }).click();
  }

  for (const page of pages) {
    await expect
      .poll(() => page.locator("video").evaluate((video) => !video.paused))
      .toBeTruthy();
  }

  await pages[1].getByRole("button", { name: "Pause everyone" }).click();
  for (const page of pages) {
    await expect
      .poll(() => page.locator("video").evaluate((video) => video.paused))
      .toBeTruthy();
  }

  await pages[0].getByLabel("Movie position").focus();
  await pages[0].getByLabel("Movie position").press("ArrowRight");
  await expect
    .poll(async () => {
      const positions = await Promise.all(
        pages.map((page) =>
          page.locator("video").evaluate((video) => video.currentTime),
        ),
      );

      return Math.abs(positions[0] - positions[1]);
    })
    .toBeLessThan(0.8);
  await pages[0].getByRole("tab", { name: "Transcode", exact: true }).click();
  pages[0].on("dialog", (dialog) => dialog.accept());
  await pages[0]
    .getByRole("button", { name: /Start transcoding|Replace transcode/ })
    .click();
  await expect
    .poll(
      async () =>
        (await (await contexts[0].request.get("/room/info")).json()).room
          .transcode.available,
    )
    .toBeGreaterThan(0);
  await pages[0]
    .getByRole("button", { name: "Transcoded", exact: true })
    .click();
  for (const page of pages) {
    await expect(
      page.getByText("TRANSCODED MEDIA", { exact: true }),
    ).toBeVisible();
  }

  await pages[0].getByRole("button", { name: "Play together" }).click();
  for (const page of pages) {
    await expect
      .poll(() => page.locator("video").evaluate((video) => !video.paused), {
        timeout: 20000,
      })
      .toBeTruthy();
  }

  await pages[0].screenshot({
    path: "/private/tmp/watchd-fixtures/playing.png",
    fullPage: true,
  });
  await pages[0].getByRole("button", { name: "Pause everyone" }).click();
  for (const page of pages) {
    await expect
      .poll(() => page.locator("video").evaluate((video) => video.paused))
      .toBeTruthy();
  }

  expect(errors).toEqual([]);
  await Promise.all(contexts.map((context) => context.close()));
});
