/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import {
  CodeBlock,
  CodeBlockCopyButton,
} from '@/components/ai-elements/code-block'
import { CopyButton } from '@/components/copy-button'
import { PublicLayout } from '@/components/layout'
import { RichContent } from '@/components/rich-content'
import { Button } from '@/components/ui/button'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'

export function Documentation() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const { systemName } = useSystemConfig()
  const configuredAddress =
    typeof status?.server_address === 'string'
      ? status.server_address.trim()
      : ''
  const serverAddress = (configuredAddress || window.location.origin)
    .replace(/\/+$/, '')
    .replace(/\/v1$/i, '')
  const apiBaseUrl = `${serverAddress}/v1`
  const sections = [
    {
      id: 'quick-start',
      title: t('footer.columns.docs.links.quickStart'),
      content: t(
        '1. [Sign in](/sign-in), then create a key in [API Keys](/keys). Choose the appropriate group, quota and model permissions.\n2. Check [Model Square](/pricing) for available models and supported endpoints. Use the exact model ID shown there.\n3. Configure your client with the API Base URL below and your API key. After a request, check [Usage Logs](/usage-logs) for usage and errors.'
      ),
    },
    {
      id: 'client-setup',
      title: t('Client configuration'),
      content: t(
        'For an OpenAI-compatible client, choose a custom provider and fill in the **API Base URL**, **API key**, and **model ID**. If the client automatically appends `/v1`, enter the site address without `/v1` to avoid a duplicated path.\n\nSend your key in the `Authorization: Bearer YOUR_API_KEY` header. Keep it on your server or in a local environment variable; do not publish it in browser code or a public repository.'
      ),
    },
    {
      id: 'api-reference',
      title: t('footer.columns.docs.links.apiDocs'),
      content: t(
        '| Method | Path | Purpose |\n| --- | --- | --- |\n| GET | `/v1/models` | List models available to your key |\n| POST | `/v1/chat/completions` | Chat Completions |\n| POST | `/v1/responses` | Responses |\n| POST | `/v1/images/generations` | Image generation |\n| POST | `/v1/embeddings` | Text embeddings |\n\nEndpoint availability depends on the selected model, your key permissions and the channels configured by the administrator. See the model details in [Model Square](/pricing) for supported endpoints and model-specific examples.'
      ),
    },
    {
      id: 'examples',
      title: t('Code samples'),
      content: t(
        'Run these cURL examples in a terminal. Replace the placeholder key and model ID with your own values. Use a model that supports the endpoint in each example. To stream a response, set `stream` to `true` in the JSON body and add `-N` to cURL.'
      ),
    },
    {
      id: 'faq',
      title: t('FAQ'),
      content: t(
        '### Authentication failed\nCheck that the key is enabled, has not expired, and is sent with the `Bearer` prefix. Use an API key from this site.\n\n### Model unavailable or access denied\nCheck the model ID, supported endpoint, key group and model restrictions. If the problem persists, contact the site administrator.\n\n### Insufficient quota or rate limit\nCheck your key quota and account balance in [Wallet](/wallet). For rate limits, reduce concurrent requests and retry after a delay.\n\n### Other request errors\nCheck the response error message and [Usage Logs](/usage-logs). When contacting the administrator, include the request time, model and request ID if available; never share your API key.'
      ),
    },
  ]
  const samples = [
    {
      title: t('Set environment variables'),
      code: [
        `export OPENAI_BASE_URL='${apiBaseUrl.replaceAll("'", "'\\''")}'`,
        "export OPENAI_API_KEY='YOUR_API_KEY'",
      ].join('\n'),
    },
    {
      title: 'GET /v1/models',
      code: 'curl "$OPENAI_BASE_URL/models" \\\n  -H "Authorization: Bearer $OPENAI_API_KEY"',
    },
    {
      title: 'POST /v1/chat/completions',
      code: 'curl "$OPENAI_BASE_URL/chat/completions" \\\n  -H "Authorization: Bearer $OPENAI_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d \'{\n    "model": "YOUR_MODEL_ID",\n    "messages": [{"role": "user", "content": "Hello"}]\n  }\'',
    },
    {
      title: 'POST /v1/responses',
      code: 'curl "$OPENAI_BASE_URL/responses" \\\n  -H "Authorization: Bearer $OPENAI_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d \'{\n    "model": "YOUR_MODEL_ID",\n    "input": "Hello"\n  }\'',
    },
  ]

  return (
    <PublicLayout>
      <div className='mx-auto max-w-6xl py-6 md:py-12'>
        <header className='mb-10 space-y-4'>
          <h1 className='text-3xl font-bold tracking-tight md:text-4xl'>
            {t('{{name}} Documentation', { name: systemName })}
          </h1>
          <p className='text-muted-foreground max-w-2xl leading-relaxed'>
            {t(
              'Connect to this site, configure your client, and make your first API request.'
            )}
          </p>
          <div className='flex flex-wrap gap-3'>
            <Button render={<Link to='/keys' />}>{t('API Keys')}</Button>
            <Button variant='outline' render={<Link to='/pricing' />}>
              {t('Model Square')}
            </Button>
          </div>
        </header>

        <div className='grid min-w-0 gap-8 lg:grid-cols-[12rem_minmax(0,1fr)] lg:gap-12'>
          <aside>
            <nav
              aria-label={t('On this page')}
              className='space-y-1 lg:sticky lg:top-24'
            >
              <p className='text-muted-foreground px-3 pb-2 text-xs font-medium'>
                {t('On this page')}
              </p>
              {sections.map((section) => (
                <Link
                  key={section.id}
                  to='/docs'
                  hash={section.id}
                  className='text-muted-foreground hover:bg-muted hover:text-foreground block rounded-md px-3 py-2 text-sm transition-colors'
                >
                  {section.title}
                </Link>
              ))}
            </nav>
          </aside>

          <div className='min-w-0 space-y-12'>
            {sections.map((section) => (
              <section
                key={section.id}
                id={section.id}
                aria-labelledby={`${section.id}-title`}
                className='scroll-mt-24 space-y-4'
              >
                <h2
                  id={`${section.id}-title`}
                  className='text-xl font-semibold tracking-tight'
                >
                  {section.title}
                </h2>
                {section.id === 'client-setup' && (
                  <div className='bg-muted/40 rounded-lg border p-4'>
                    <p className='text-muted-foreground mb-2 text-xs font-medium'>
                      {t('API Base URL')}
                    </p>
                    <div className='flex min-w-0 items-center gap-3'>
                      <code className='min-w-0 flex-1 text-sm break-all'>
                        {apiBaseUrl}
                      </code>
                      <CopyButton
                        value={apiBaseUrl}
                        aria-label={t('Copy API URL')}
                      />
                    </div>
                  </div>
                )}
                <RichContent content={section.content} />
                {section.id === 'examples' &&
                  samples.map((sample) => (
                    <CodeBlock
                      key={sample.title}
                      title={sample.title}
                      code={sample.code}
                      language='bash'
                      showToolbar
                      enableCollapse={false}
                    >
                      <CodeBlockCopyButton />
                    </CodeBlock>
                  ))}
              </section>
            ))}
          </div>
        </div>
      </div>
    </PublicLayout>
  )
}
