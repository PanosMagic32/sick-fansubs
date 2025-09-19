import { Type } from 'class-transformer';
import { IsDate, IsEnum, IsNumber, IsOptional, IsString, Min } from 'class-validator';

export class SearchDto {
  @IsOptional()
  @IsString()
  searchTerm?: string;

  @IsOptional()
  @IsEnum(['blog-post', 'project', 'all'])
  type?: 'blog-post' | 'project' | 'all' = 'all';

  @IsOptional()
  @Type(() => Date)
  @IsDate()
  dateFrom?: Date;

  @IsOptional()
  @Type(() => Date)
  @IsDate()
  dateTo?: Date;

  @Type(() => Number)
  @IsNumber()
  @Min(1)
  pageSize: number = 10;

  @Type(() => Number)
  @IsNumber()
  @Min(0)
  page: number = 0;
}
