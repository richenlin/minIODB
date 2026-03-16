import { apiClient } from './client'
import {
  BrowseParams,
  BrowseResult,
  CreateTableRequest,
  QueryResult,
  TableDetailResult,
  TableResult,
  TableStatsResult,
  UpdateRecordRequest,
  UpdateTableRequest,
  WriteRecordRequest,
} from './types'

export const dataApi = {
  listTables: () => apiClient.get<TableResult[]>('/tables'),
  
  getTable: (name: string) =>
    apiClient.get<TableDetailResult>(`/tables/${encodeURIComponent(name)}`),
  
  createTable: (req: CreateTableRequest) =>
    apiClient.post<TableDetailResult>('/tables', req),
  
  updateTable: (name: string, req: UpdateTableRequest) =>
    apiClient.put<TableDetailResult>(`/tables/${encodeURIComponent(name)}`, req),
  
  deleteTable: (name: string) =>
    apiClient.delete<void>(`/tables/${encodeURIComponent(name)}`),
  
  getTableStats: (name: string) =>
    apiClient.get<TableStatsResult>(`/tables/${encodeURIComponent(name)}/stats`),
  
  browseData: (table: string, params: BrowseParams) => {
    const query = new URLSearchParams()
    if (params.page !== undefined) query.set('page', String(params.page))
    if (params.page_size !== undefined) query.set('page_size', String(params.page_size))
    if (params.sort_by) query.set('sort_by', params.sort_by)
    if (params.sort_order) query.set('sort_order', params.sort_order)
    if (params.filter) query.set('filter', params.filter)
    if (params.id) query.set('id', params.id)
    const queryString = query.toString()
    return apiClient.get<BrowseResult>(
      `/tables/${encodeURIComponent(table)}/data${queryString ? `?${queryString}` : ''}`
    )
  },
  
  writeRecord: (table: string, req: WriteRecordRequest) =>
    apiClient.post<Record<string, unknown>>(`/tables/${encodeURIComponent(table)}/data`, req),
  
  writeBatch: (table: string, records: WriteRecordRequest[]) =>
    apiClient.post<Record<string, unknown>[]>(
      `/tables/${encodeURIComponent(table)}/data/batch`,
      { records }
    ),
  
  updateRecord: (table: string, id: string, req: UpdateRecordRequest) =>
    apiClient.put<Record<string, unknown>>(
      `/tables/${encodeURIComponent(table)}/data/${encodeURIComponent(id)}`,
      req
    ),
  
  deleteRecord: (table: string, id: string, day?: string) => {
    const query = day ? `?day=${encodeURIComponent(day)}` : ''
    return apiClient.delete<void>(
      `/tables/${encodeURIComponent(table)}/data/${encodeURIComponent(id)}${query}`
    )
  },
  
  querySQL: (sql: string) =>
    apiClient.post<QueryResult>('/query', { sql }),
}
