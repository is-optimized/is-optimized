# Twilio campaign record

**Status:** submitted; in review as of 2026-10-31. SMS remains disabled in production.

Sire Run's A2P brand is approved. Twilio accepted the Low Volume Mixed campaign registration at 2026-10-30T08:20:56Z with status `IN_PROGRESS`, no errors, and campaign SID `QE2c6890da8086d771620e9b13fadeba0b`. The owner confirmed that the console still showed “in review” on 2026-10-31. Campaign review is not approval. No SMS was sent.

The application included the existing Privacy Policy and Terms URLs and hosted prelaunch screenshots. Those screenshots used synthetic values and are not proof that the live product's SMS flow is active. The submitted campaign must be reconciled with the final production flow before sending.

## Approved product copy

Optional marketing disclosure:

> By entering your mobile number and sending the text message below, you agree that Sire Run, Inc. may contact you at that number by phone call, text message, and artificial or prerecorded voice (including voicemail drops) about AI services for your business. Consent is not required to receive the free website audit report or to purchase anything. Message and data rates may apply. Reply STOP to any text to opt out.

Pre-filled request the visitor sends:

> Hi, please send me the free website audit report for [domain].

Reply sent after Twilio receives a valid report request:

> Sire Run, Inc.: Here’s your free website audit report: [link]. Reply STOP to opt out.

The code appends an expiring correlation code to the pre-filled request. The visitor chooses whether to send it. The authenticated inbound sender must match the mobile number entered on the page. The optional marketing choice is separate from the requested report delivery. Visitors may skip text and read the HTML report without a phone number or marketing opt-in.

## Public policy and prelaunch evidence

- Privacy: https://sire.run/privacy/
- Terms: https://sire.run/terms/
- Prelaunch consent screenshot: https://raw.githubusercontent.com/is-optimized/is-optimized/campaign/twilio-optin-evidence-20261030/docs/twilio-campaign-evidence/01-consent-flow.png
- Prelaunch text handoff screenshot: https://raw.githubusercontent.com/is-optimized/is-optimized/campaign/twilio-optin-evidence-20261030/docs/twilio-campaign-evidence/02-text-handoff.png

The screenshots are evidence of the prelaunch rendering only, not current production behavior. No duplicate campaign has been submitted and no additional fee is claimed by this record. A prior Twilio balance display was USD 54.3398 after submission; no immediate pre-submission balance was captured, so this note cannot attribute a fee posting.

## Remaining before SMS can be enabled

- Twilio must approve the submitted campaign.
- Configure the Auth Token through a pinned Secret Manager version and qualify secret access on the deployed runtime; never put the credential in source or image metadata.
- Confirm the approved sender, public webhook URL, callback settings, and production wording/version.
- Deploy the reviewed code separately and verify the approved flow with an authorized test number before opening it to visitors.
