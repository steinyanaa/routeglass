import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'
import App from './app'
import './i18n'
import './styles.css'
import { initializeTheme } from './theme'

initializeTheme()

const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 15_000, retry: 1, refetchOnWindowFocus: false } },
})

createRoot(document.getElementById('app')!).render(
  <QueryClientProvider client={queryClient}>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </QueryClientProvider>,
)
