import { Controller, Get, Query } from '@nestjs/common';
import { ApiTags } from '@nestjs/swagger';
import { ApiSearchService } from './api-search.service';
import { SearchDto } from './dtos/search.dto';

@ApiTags('Search')
@Controller('search')
export class ApiSearchController {
  constructor(private readonly searchService: ApiSearchService) {}

  @Get()
  async search(@Query() searchDto: SearchDto) {
    const result = await this.searchService.search({
      searchTerm: searchDto.searchTerm,
      type: searchDto.type,
      pagination: { page: searchDto.page, pageSize: searchDto.pageSize },
    });

    return result;
  }
}
