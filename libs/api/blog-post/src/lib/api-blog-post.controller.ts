import {
  UnauthorizedException,
  Body,
  Controller,
  Delete,
  Get,
  Param,
  Patch,
  Post,
  Query,
  Req,
  UseGuards,
} from '@nestjs/common';
import { ApiParam, ApiTags } from '@nestjs/swagger';
import { Throttle } from '@nestjs/throttler';

import { PaginationDto } from '@api/shared/dto';
import { ParseMongoIdPipe } from '@api/shared';
import { AdminGuard, JwtAuthGuard } from '@api/user';

import { ApiBlogPostService } from './api-blog-post.service';
import { CreateBlogPostDto } from './dtos/create-blog-post.dto';
import { UpdateBlogPostDto } from './dtos/update-blog-post.dto';
import { BlogPost } from './schemas/blog-post.schema';

@ApiTags('Blog-Post')
@Controller('blog-post')
export class ApiBlogPostController {
  constructor(private readonly apiBlogPostService: ApiBlogPostService) {}

  private getActorIdFromRequest(req: { user?: { sub?: string } }): string {
    if (!req.user?.sub) {
      throw new UnauthorizedException('User not authenticated.');
    }
    return req.user.sub;
  }

  @Post()
  @UseGuards(JwtAuthGuard, AdminGuard)
  @Throttle({ default: { limit: 30, ttl: 60000 } })
  async create(@Body() createBlogPostDto: CreateBlogPostDto, @Req() req: { user?: { sub?: string } }) {
    return this.apiBlogPostService.create(createBlogPostDto, this.getActorIdFromRequest(req));
  }

  @Get()
  async findAll(@Query() pagination: PaginationDto): Promise<{ posts: BlogPost[]; count: number }> {
    return this.apiBlogPostService.findAll(pagination.pagesize, pagination.page);
  }

  @ApiParam({ name: 'id' })
  @Get(':id')
  async findOne(@Param('id', ParseMongoIdPipe) id: string): Promise<BlogPost | undefined> {
    return this.apiBlogPostService.findOne(id);
  }

  @ApiParam({ name: 'id' })
  @Patch(':id')
  @UseGuards(JwtAuthGuard, AdminGuard)
  @Throttle({ default: { limit: 20, ttl: 60000 } })
  async update(
    @Param('id', ParseMongoIdPipe) id: string,
    @Body() updateBlogPostDto: UpdateBlogPostDto,
    @Req() req: { user?: { sub?: string } },
  ) {
    return this.apiBlogPostService.update(id, updateBlogPostDto, this.getActorIdFromRequest(req));
  }

  @ApiParam({ name: 'id' })
  @Delete(':id')
  @UseGuards(JwtAuthGuard, AdminGuard)
  @Throttle({ default: { limit: 20, ttl: 60000 } })
  async delete(@Param('id', ParseMongoIdPipe) id: string, @Req() req: { user?: { sub?: string } }) {
    return this.apiBlogPostService.delete(id, this.getActorIdFromRequest(req));
  }
}
