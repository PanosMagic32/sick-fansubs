import { Body, Controller, Delete, Get, Param, Patch, Post, Query } from '@nestjs/common';
import { ApiParam, ApiTags } from '@nestjs/swagger';

import { ApiBlogPostService } from './api-blog-post.service';
import { CreateBlogPostDto } from './dtos/create-blog-post.dto';
import { UpdateBlogPostDto } from './dtos/update-blog-post.dto';
import { BlogPost } from './schemas/blog-post.schema';

@ApiTags('Blog-Post')
@Controller('blog-post')
export class ApiBlogPostController {
  constructor(private readonly apiBlogPostService: ApiBlogPostService) {}

  @Post()
  async create(@Body() createBlogPostDto: CreateBlogPostDto) {
    return this.apiBlogPostService.create(createBlogPostDto);
  }

  @Get()
  async findAll(
    @Query() params: { pagesize: number; page: number; startId?: string }
  ): Promise<{ posts: BlogPost[]; count: number }> {
    return this.apiBlogPostService.findAll(params.pagesize, params.page);
  }

  @Get('search')
  async search(@Query() s: string) {
    let options = {};
    console.log(s);

    if (s) {
      options = {
        $or: [{ title: new RegExp(s, 'i') }, { subtitle: new RegExp(s, 'i') }, { description: new RegExp(s, 'i') }],
      };

      const query = this.apiBlogPostService.search(options);

      return query;
    }

    return this.apiBlogPostService.findAll(5, 0);
  }

  @ApiParam({ name: 'id' })
  @Get(':id')
  async findOne(@Param('id') id: string): Promise<BlogPost | undefined> {
    return this.apiBlogPostService.findOne(id);
  }

  @ApiParam({ name: 'id' })
  @Patch(':id')
  async update(@Param('id') id: string, @Body() updateBlogPostDto: UpdateBlogPostDto) {
    return this.apiBlogPostService.update(id, updateBlogPostDto);
  }

  @ApiParam({ name: 'id' })
  @Delete(':id')
  async delete(@Param('id') id: string) {
    return this.apiBlogPostService.delete(id);
  }
}
