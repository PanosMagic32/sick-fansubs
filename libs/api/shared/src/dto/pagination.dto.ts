import { Type } from 'class-transformer';
import { IsInt, IsOptional, Max, Min } from 'class-validator';

/**
 * Shared pagination DTO for list endpoints.
 * Defaults to page 0 (first page), 10 items per page, max 100.
 */
export class PaginationDto {
  @Type(() => Number)
  @IsInt()
  @Min(0)
  @IsOptional()
  page = 0;

  @Type(() => Number)
  @IsInt()
  @Min(1)
  @Max(100)
  @IsOptional()
  pagesize = 10;
}
