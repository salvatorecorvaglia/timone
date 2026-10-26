import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App } from './app/App';
import { UpdaterProvider } from './lib/updater';
import './styles.css';
const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 10000, refetchOnWindowFocus: false } },
});
ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <UpdaterProvider>
        <BrowserRouter useTransitions={false}>
          <App />
        </BrowserRouter>
      </UpdaterProvider>
    </QueryClientProvider>
  </React.StrictMode>,
);
