<script setup lang="ts">
definePageMeta({ title: 'Privacy Policy' })
</script>

<template>
  <UContainer class="py-10 max-w-3xl">
    <h1 class="text-2xl font-bold text-highlighted mb-2">Privacy Policy</h1>
    <p class="text-xs text-muted mb-8">Last updated: {{ new Date().getFullYear() }}</p>

    <div class="prose prose-sm dark:prose-invert max-w-none space-y-8 text-default text-sm leading-relaxed">

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">1. Overview</h2>
        <p>
          This Privacy Policy explains how Media Server Pro ("we", "us", "our") collects, uses, and protects
          information when you access this service. By using the service you agree to the practices described here.
          This service hosts adult content and is strictly restricted to users who are 18 years of age or older.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">2. Information We Collect</h2>
        <ul class="list-disc pl-5 space-y-1">
          <li><strong>Account data:</strong> username, hashed password (never stored in plain text), and optional email address provided during registration.</li>
          <li><strong>Session data:</strong> a temporary session identifier stored in an HttpOnly cookie to keep you logged in.</li>
          <li><strong>Usage data:</strong> watch history, favorites, and playback preferences you create while using the service.</li>
          <li><strong>Analytics data</strong> (only with your consent): aggregate usage statistics such as page views and playback events. This data does not identify you personally.</li>
          <li><strong>Age verification:</strong> a cookie confirming you have verified you are 18 or older. No date of birth or government ID is stored.</li>
          <li><strong>Technical data:</strong> IP address, browser type, and request timestamps — used solely for security, rate limiting, and abuse prevention.</li>
        </ul>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">3. Cookies</h2>
        <p class="mb-2">We use the following cookies:</p>
        <div class="overflow-x-auto">
          <table class="w-full text-xs border-collapse">
            <thead>
              <tr class="border-b border-default text-left">
                <th class="py-1.5 pr-4 font-semibold">Cookie</th>
                <th class="py-1.5 pr-4 font-semibold">Purpose</th>
                <th class="py-1.5 pr-4 font-semibold">Duration</th>
                <th class="py-1.5 font-semibold">Required</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-default">
              <tr>
                <td class="py-1.5 pr-4 font-mono">session_id</td>
                <td class="py-1.5 pr-4">Maintains your login session</td>
                <td class="py-1.5 pr-4">Session / 30 days</td>
                <td class="py-1.5">Yes</td>
              </tr>
              <tr>
                <td class="py-1.5 pr-4 font-mono">age_verified</td>
                <td class="py-1.5 pr-4">Records that you have passed age verification</td>
                <td class="py-1.5 pr-4">1 year</td>
                <td class="py-1.5">Yes (if age gate enabled)</td>
              </tr>
              <tr>
                <td class="py-1.5 pr-4 font-mono">cookie_consent</td>
                <td class="py-1.5 pr-4">Stores your cookie preference choice</td>
                <td class="py-1.5 pr-4">1 year</td>
                <td class="py-1.5">Yes</td>
              </tr>
              <tr>
                <td class="py-1.5 pr-4 font-mono">Analytics</td>
                <td class="py-1.5 pr-4">Aggregate, anonymous usage statistics</td>
                <td class="py-1.5 pr-4">Session</td>
                <td class="py-1.5">No (optional)</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="mt-2">
          You may withdraw your consent to optional analytics cookies at any time by clearing your
          <span class="font-mono text-xs">cookie_consent</span> cookie or contacting the site administrator.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">4. How We Use Your Data</h2>
        <ul class="list-disc pl-5 space-y-1">
          <li>To authenticate you and maintain your session securely.</li>
          <li>To provide personalized features (watch history, favorites, recommendations).</li>
          <li>To enforce age restrictions required for access to adult content.</li>
          <li>To protect the service from abuse, unauthorized access, and fraudulent activity.</li>
          <li>To improve the service using anonymized, aggregated analytics (only with your consent).</li>
        </ul>
        <p class="mt-2">We do not sell, rent, or share your personal data with third parties for marketing purposes.</p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">5. Legal Basis for Processing (GDPR)</h2>
        <p>Where the GDPR applies, we process your data under the following legal bases:</p>
        <ul class="list-disc pl-5 space-y-1 mt-2">
          <li><strong>Contract:</strong> processing necessary to provide the service you requested (account, session, playback).</li>
          <li><strong>Legitimate interest:</strong> security logging, rate limiting, and abuse prevention.</li>
          <li><strong>Consent:</strong> optional analytics cookies, which you can withdraw at any time.</li>
          <li><strong>Legal obligation:</strong> age verification requirements under applicable laws governing adult content.</li>
        </ul>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">6. Your Rights</h2>
        <p>Depending on your jurisdiction, you may have the right to:</p>
        <ul class="list-disc pl-5 space-y-1 mt-2">
          <li>Access the personal data we hold about you.</li>
          <li>Correct inaccurate data.</li>
          <li>Request deletion of your data ("right to be forgotten").</li>
          <li>Restrict or object to certain processing.</li>
          <li>Data portability (receive your data in a structured, machine-readable format).</li>
          <li>Withdraw consent at any time without affecting prior processing.</li>
        </ul>
        <p class="mt-2">
          To exercise these rights, use the
          <NuxtLink to="/profile" class="underline hover:text-highlighted">Profile page</NuxtLink>
          (data deletion request, account deletion) or contact the site administrator.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">7. California Residents (CCPA/CPRA)</h2>
        <p>
          California residents have additional rights under the California Consumer Privacy Act, including the right
          to know what personal information is collected, to delete it, and to opt out of its sale.
          We do not sell personal information. To exercise your rights, use the Profile page or contact the administrator.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">8. Data Retention</h2>
        <p>
          Account data is retained for as long as your account is active. Analytics data is retained for a maximum of
          30 days before automatic deletion. You may request deletion of your account and associated data at any time
          via the Profile page.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">9. Security</h2>
        <p>
          We implement industry-standard security measures including bcrypt password hashing, HttpOnly session cookies,
          CSRF protection, Content Security Policy headers, rate limiting, and IP-based abuse prevention.
          No system is 100% secure; you are responsible for keeping your credentials confidential.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">10. Children's Privacy</h2>
        <p>
          This service is strictly for adults aged 18 and over. We do not knowingly collect data from minors.
          If you believe a minor has accessed this service, please contact the administrator immediately.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">11. Changes to This Policy</h2>
        <p>
          We may update this Privacy Policy periodically. Continued use of the service after changes constitutes
          acceptance of the updated policy. The "last updated" date at the top reflects the most recent revision.
        </p>
      </section>

    </div>

    <div class="mt-10 flex gap-4 text-xs text-muted">
      <NuxtLink to="/terms" class="underline hover:text-default">Terms of Service</NuxtLink>
      <NuxtLink to="/" class="underline hover:text-default">Back to home</NuxtLink>
    </div>
  </UContainer>
</template>
